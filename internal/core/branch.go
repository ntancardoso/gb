package core

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	branchStateNoCommits = "no commits"
	branchStateDetached  = "detached"
	gitCommandTimeout    = 5 * time.Minute
	maxRetries           = 3
	retryDelay           = 2 * time.Second
)

type BranchResult struct {
	RelPath string
	Branch  string
	Error   error
}

type CommandResult struct {
	RelPath string
	Output  string
	Error   error
	Retries int
}

func executeGitCommandWithRetry(ctx context.Context, dir string, args ...string) ([]byte, int, error) {
	var lastErr error
	var output []byte

	for attempt := 0; attempt < maxRetries; attempt++ {
		cmdCtx, cancel := context.WithTimeout(ctx, gitCommandTimeout)
		cmd := exec.CommandContext(cmdCtx, "git", args...)
		cmd.Dir = dir

		output, lastErr = cmd.CombinedOutput()
		cancel()

		if lastErr == nil {
			return output, attempt, nil
		}

		if cmdCtx.Err() != nil {
			if attempt < maxRetries-1 {
				time.Sleep(retryDelay)
				continue
			}
		} else {
			return output, attempt, lastErr
		}
	}

	return output, maxRetries - 1, lastErr
}

func executeGitCommandWithRetryToFile(ctx context.Context, dir string, logFile *os.File, args ...string) (int, error) {
	var lastErr error

	for attempt := 0; attempt < maxRetries; attempt++ {
		cmdCtx, cancel := context.WithTimeout(ctx, gitCommandTimeout)
		cmd := exec.CommandContext(cmdCtx, "git", args...)
		cmd.Dir = dir

		cmd.Stdout = logFile
		cmd.Stderr = logFile

		lastErr = cmd.Run()
		cancel()

		if lastErr == nil {
			return attempt, nil
		}

		if cmdCtx.Err() != nil {
			if attempt < maxRetries-1 {
				_, _ = fmt.Fprintf(logFile, "\n--- Retry %d/%d after timeout ---\n", attempt+1, maxRetries)
				time.Sleep(retryDelay)
				continue
			}
		} else {
			_, _ = fmt.Fprintf(logFile, "\n--- Command failed: %s ---\n", lastErr)
			return attempt, lastErr
		}
	}

	return maxRetries - 1, lastErr
}

func executeShellCommandToFile(ctx context.Context, dir string, logFile *os.File, command string) error {
	cmdCtx, cancel := context.WithTimeout(ctx, gitCommandTimeout)
	defer cancel()

	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(cmdCtx, "cmd", "/c", command)
	} else {
		cmd = exec.CommandContext(cmdCtx, "sh", "-c", command)
	}
	cmd.Dir = dir

	cmd.Stdout = logFile
	cmd.Stderr = logFile

	err := cmd.Run()

	if err != nil {
		_, _ = fmt.Fprintf(logFile, "\n--- Command failed: %s ---\n", err)
	}

	return err
}

func getBranch(path string) (string, error) {
	cmd := exec.Command("git", "branch", "--show-current")
	cmd.Dir = path
	if out, err := cmd.Output(); err == nil {
		if branch := strings.TrimSpace(string(out)); branch != "" {
			return branch, nil
		}
	}

	cmd = exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
	cmd.Dir = path
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to get branch: %w", err)
	}

	branch := strings.TrimSpace(string(out))
	if branch == "HEAD" {
		cmd = exec.Command("git", "rev-parse", "--verify", "HEAD")
		cmd.Dir = path
		if err := cmd.Run(); err != nil {
			return branchStateNoCommits, nil
		}
		return branchStateDetached, nil
	}

	return branch, nil
}

func listAllBranches(root string, workers int, cfg *Config) {
	fmt.Printf("Discovering repos in %s...\n", root)
	allRepos, err := findGitRepos(root, cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
	if len(allRepos) == 0 {
		fmt.Println("No repos found")
		return
	}

	repos := cfg.filterReposForExecution(allRepos)
	if len(repos) == 0 {
		fmt.Println("No repos match the specified include/exclude criteria")
		return
	}

	fmt.Printf("Listing branches in %d repos (filtered from %d discovered)...\n", len(repos), len(allRepos))

	repoCh := make(chan RepoInfo, len(repos))
	resCh := make(chan BranchResult, len(repos))

	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for r := range repoCh {
				branch, err := getBranch(r.Path)
				resCh <- BranchResult{RelPath: r.RelPath, Branch: branch, Error: err}
			}
		}()
	}
	go func() {
		for _, r := range repos {
			repoCh <- r
		}
		close(repoCh)
	}()
	go func() {
		wg.Wait()
		close(resCh)
	}()

	branchRepos := make(map[string][]string)
	for res := range resCh {
		key := res.Branch
		if res.Error != nil {
			key = "error"
		}
		branchRepos[key] = append(branchRepos[key], res.RelPath)
	}

	var branches []string
	for b := range branchRepos {
		branches = append(branches, b)
	}
	sort.Strings(branches)

	for _, b := range branches {
		fmt.Printf("Branch: %s\n", b)
		fmt.Println("-----------------")
		sort.Strings(branchRepos[b])
		for _, repo := range branchRepos[b] {
			fmt.Println(repo)
		}
		fmt.Println("=================")
	}
}

func executeCommandInRepos(root, command string, workers int, cfg *Config) {
	fmt.Printf("Discovering repos in %s...\n", root)
	allRepos, err := findGitRepos(root, cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
	if len(allRepos) == 0 {
		fmt.Println("No repos found")
		return
	}

	repos := cfg.filterReposForExecution(allRepos)
	if len(repos) == 0 {
		fmt.Println("No repos match the specified include/exclude criteria")
		return
	}

	args := strings.Fields(command)
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "Error: Empty command")
		os.Exit(1)
	}

	fmt.Printf("Found %d repos (filtered from %d discovered), executing 'git %s' with %d workers...\n",
		len(repos), len(allRepos), command, workers)

	logManager, err := NewLogManager()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating log manager: %s\n", err)
		os.Exit(1)
	}

	progress := NewProgressState(repos, fmt.Sprintf("Executing 'git %s'", command), cfg.PageSize)
	progress.render()
	progress.StartInput()
	defer progress.StopInput()

	repoCh := make(chan RepoInfo, len(repos))
	resCh := make(chan CommandResult, len(repos))

	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for r := range repoCh {

				progress.UpdateStatus(r.RelPath, statusProcessing, "")

				logFile, err := logManager.CreateLogFile(r.RelPath)
				var retries int
				var cmdErr error

				if err != nil {

					output, retries, cmdErr := executeGitCommandWithRetry(context.Background(), r.Path, args...)
					result := CommandResult{
						RelPath: r.RelPath,
						Output:  string(output),
						Error:   cmdErr,
						Retries: retries,
					}

					if cmdErr != nil {
						errorMsg := cmdErr.Error()
						if len(errorMsg) > 50 {
							errorMsg = errorMsg[:50] + "..."
						}
						progress.UpdateStatus(r.RelPath, statusFailed, errorMsg)
					} else {
						progress.UpdateStatus(r.RelPath, statusCompleted, "")
					}

					resCh <- result
				} else {

					retries, cmdErr = executeGitCommandWithRetryToFile(context.Background(), r.Path, logFile, args...)
					_ = logFile.Close()

					result := CommandResult{
						RelPath: r.RelPath,
						Output:  "",
						Error:   cmdErr,
						Retries: retries,
					}

					if cmdErr != nil {
						errorMsg := cmdErr.Error()
						if len(errorMsg) > 50 {
							errorMsg = errorMsg[:50] + "..."
						}
						progress.UpdateStatus(r.RelPath, statusFailed, errorMsg)
					} else {
						progress.UpdateStatus(r.RelPath, statusCompleted, "")
					}

					resCh <- result
				}
			}
		}()
	}
	go func() {
		for _, r := range repos {
			repoCh <- r
		}
		close(repoCh)
	}()
	go func() {
		wg.Wait()
		close(resCh)
	}()

	var results []CommandResult
	success, failed := 0, 0
	for res := range resCh {
		results = append(results, res)
		if res.Error != nil {
			failed++
		} else {
			success++
		}
	}

	progress.RenderFinal()

	fmt.Printf("\n--- Summary ---\n")
	fmt.Printf("Executed 'git %s' in %d repos: %d succeeded, %d failed\n",
		command, success+failed, success, failed)

	if PromptViewLogs() {
		DisplayLogs(logManager, results)
	} else {
		fmt.Printf("\nLogs are available at: %s\n", logManager.GetTempDir())
		fmt.Println("You can review them later if needed.")
	}

	if failed > 0 {
		os.Exit(1)
	}
}

func executeShellInRepos(root, command string, workers int, cfg *Config) {
	fmt.Printf("Discovering repos in %s...\n", root)
	allRepos, err := findGitRepos(root, cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
	if len(allRepos) == 0 {
		fmt.Println("No repos found")
		return
	}

	repos := cfg.filterReposForExecution(allRepos)
	if len(repos) == 0 {
		fmt.Println("No repos match the specified include/exclude criteria")
		return
	}

	if strings.TrimSpace(command) == "" {
		fmt.Fprintln(os.Stderr, "Error: Empty command")
		os.Exit(1)
	}

	fmt.Printf("Found %d repos (filtered from %d discovered), executing '%s' with %d workers...\n",
		len(repos), len(allRepos), command, workers)

	logManager, err := NewLogManager()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating log manager: %s\n", err)
		os.Exit(1)
	}

	progress := NewProgressState(repos, fmt.Sprintf("Executing '%s'", command), cfg.PageSize)
	progress.render()
	progress.StartInput()
	defer progress.StopInput()

	repoCh := make(chan RepoInfo, len(repos))
	resCh := make(chan CommandResult, len(repos))

	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for r := range repoCh {

				progress.UpdateStatus(r.RelPath, statusProcessing, "")

				logFile, err := logManager.CreateLogFile(r.RelPath)
				var cmdErr error

				if err != nil {

					ctx, cancel := context.WithTimeout(context.Background(), gitCommandTimeout)
					var cmd *exec.Cmd
					if runtime.GOOS == "windows" {
						cmd = exec.CommandContext(ctx, "cmd", "/c", command)
					} else {
						cmd = exec.CommandContext(ctx, "sh", "-c", command)
					}
					cmd.Dir = r.Path
					output, cmdErr := cmd.CombinedOutput()
					cancel()

					result := CommandResult{
						RelPath: r.RelPath,
						Output:  string(output),
						Error:   cmdErr,
					}

					if cmdErr != nil {
						errorMsg := cmdErr.Error()
						if len(errorMsg) > 50 {
							errorMsg = errorMsg[:50] + "..."
						}
						progress.UpdateStatus(r.RelPath, statusFailed, errorMsg)
					} else {
						progress.UpdateStatus(r.RelPath, statusCompleted, "")
					}

					resCh <- result
				} else {

					cmdErr = executeShellCommandToFile(context.Background(), r.Path, logFile, command)
					_ = logFile.Close()

					result := CommandResult{
						RelPath: r.RelPath,
						Output:  "",
						Error:   cmdErr,
					}

					if cmdErr != nil {
						errorMsg := cmdErr.Error()
						if len(errorMsg) > 50 {
							errorMsg = errorMsg[:50] + "..."
						}
						progress.UpdateStatus(r.RelPath, statusFailed, errorMsg)
					} else {
						progress.UpdateStatus(r.RelPath, statusCompleted, "")
					}

					resCh <- result
				}
			}
		}()
	}
	go func() {
		for _, r := range repos {
			repoCh <- r
		}
		close(repoCh)
	}()
	go func() {
		wg.Wait()
		close(resCh)
	}()

	var results []CommandResult
	success, failed := 0, 0
	for res := range resCh {
		results = append(results, res)
		if res.Error != nil {
			failed++
		} else {
			success++
		}
	}

	progress.RenderFinal()

	fmt.Printf("\n--- Summary ---\n")
	fmt.Printf("Executed '%s' in %d repos: %d succeeded, %d failed\n",
		command, success+failed, success, failed)

	if PromptViewLogs() {
		DisplayLogs(logManager, results)
	} else {
		fmt.Printf("\nLogs are available at: %s\n", logManager.GetTempDir())
		fmt.Println("You can review them later if needed.")
	}

	if failed > 0 {
		os.Exit(1)
	}
}
