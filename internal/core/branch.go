package core

import (
	"context"
	"errors"
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

var errReposFailed = errors.New("one or more repos failed")

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
	Skipped bool
}

func executeGitCommandWithRetry(ctx context.Context, dir string, args ...string) ([]byte, int, error) {
	var lastErr error
	var output []byte

	for attempt := range maxRetries {
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

	for attempt := range maxRetries {
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

func (cfg *Config) filterReposByBranch(repos []RepoInfo, workers int) []RepoInfo {
	if len(cfg.includeBranchSet) == 0 && len(cfg.excludeBranchSet) == 0 &&
		len(cfg.includeBranchPats) == 0 && len(cfg.excludeBranchPats) == 0 {
		return repos
	}

	type result struct {
		repo   RepoInfo
		branch string
	}

	repoCh := make(chan RepoInfo, len(repos))
	resCh := make(chan result, len(repos))

	var wg sync.WaitGroup
	for range workers {
		wg.Go(func() {
			for r := range repoCh {
				branch, _ := getBranch(r.Path)
				resCh <- result{repo: r, branch: branch}
			}
		})
	}
	for _, r := range repos {
		repoCh <- r
	}
	close(repoCh)
	go func() { wg.Wait(); close(resCh) }()

	var filtered []RepoInfo
	for res := range resCh {
		if cfg.shouldExecuteInBranch(res.branch) {
			filtered = append(filtered, res.repo)
		}
	}
	return filtered
}

func listAllBranches(ctx context.Context, root string, workers int, cfg *Config) error {
	repos, total := discoverRepos(root, workers, cfg, false)
	if repos == nil {
		return nil
	}

	fmt.Println(StyleInfo.Render(fmt.Sprintf("Listing branches in %d repos (filtered from %d discovered)...", len(repos), total)))

	results := runPool(ctx, repos, workers, func(_ context.Context, r RepoInfo) BranchResult {
		branch, err := getBranch(r.Path)
		return BranchResult{RelPath: r.RelPath, Branch: branch, Error: err}
	})

	branchRepos := make(map[string][]string)
	for _, res := range results {
		key := res.Branch
		if res.Error != nil {
			key = "error"
		}
		branchRepos[key] = append(branchRepos[key], res.RelPath)
	}

	branches := make([]string, 0, len(branchRepos))
	for b := range branchRepos {
		branches = append(branches, b)
	}
	sort.Strings(branches)

	for _, b := range branches {
		fmt.Printf("%s %s\n", StyleBold.Render("Branch:"), StyleSuccess.Render(b))
		fmt.Println(StyleDim.Render("-----------------"))
		sort.Strings(branchRepos[b])
		for _, repo := range branchRepos[b] {
			fmt.Println(repo)
		}
		fmt.Println(StyleDim.Render("================="))
	}
	return nil
}

func executeCommandInRepos(ctx context.Context, root, command string, workers int, cfg *Config) error {
	repos, total := discoverRepos(root, workers, cfg, false)
	if repos == nil {
		return nil
	}

	args := strings.Fields(command)
	if len(args) == 0 {
		return fmt.Errorf("empty command")
	}

	fmt.Println(StyleInfo.Render(fmt.Sprintf("Found %d repos (filtered from %d discovered), executing 'git %s' with %d workers...",
		len(repos), total, command, min(workers, len(repos)))))

	logManager, err := NewLogManager()
	if err != nil {
		return fmt.Errorf("log manager: %w", err)
	}

	progress := NewProgressState(repos, fmt.Sprintf("Executing 'git %s'", command), cfg.PageSize)
	stop := progress.start()

	results := runPool(ctx, repos, workers, func(ctx context.Context, r RepoInfo) CommandResult {
		progress.UpdateStatus(r.RelPath, statusProcessing, "")

		logFile, logErr := logManager.CreateLogFile(r.RelPath)
		if logErr != nil {
			output, retries, cmdErr := executeGitCommandWithRetry(ctx, r.Path, args...)
			st, msg := progressStatusFromErr(cmdErr)
			progress.UpdateStatus(r.RelPath, st, msg)
			return CommandResult{RelPath: r.RelPath, Output: string(output), Error: cmdErr, Retries: retries}
		}
		retries, cmdErr := executeGitCommandWithRetryToFile(ctx, r.Path, logFile, args...)
		_ = logFile.Close()
		st, msg := progressStatusFromErr(cmdErr)
		progress.UpdateStatus(r.RelPath, st, msg)
		return CommandResult{RelPath: r.RelPath, Error: cmdErr, Retries: retries}
	})

	success, failed := 0, 0
	for _, res := range results {
		if res.Error != nil {
			failed++
		} else {
			success++
		}
	}

	stop()

	fmt.Println("\n" + StyleBold.Render("--- Summary ---"))
	fmt.Printf("Executed 'git %s' in %d repos: %s succeeded, %s failed\n",
		command, success+failed,
		StyleSuccess.Render(fmt.Sprintf("%d", success)),
		StyleFailed.Render(fmt.Sprintf("%d", failed)))

	if PromptViewLogs() {
		DisplayLogs(logManager, results)
	} else {
		fmt.Printf("\nLogs are available at: %s\n", logManager.GetTempDir())
		fmt.Println("You can review them later if needed.")
	}

	if failed > 0 {
		return errReposFailed
	}
	return nil
}

func executeShellInRepos(ctx context.Context, root, command string, workers int, cfg *Config) error {
	repos, total := discoverRepos(root, workers, cfg, false)
	if repos == nil {
		return nil
	}

	if strings.TrimSpace(command) == "" {
		return fmt.Errorf("empty command")
	}

	fmt.Println(StyleInfo.Render(fmt.Sprintf("Found %d repos (filtered from %d discovered), executing '%s' with %d workers...",
		len(repos), total, command, min(workers, len(repos)))))

	logManager, err := NewLogManager()
	if err != nil {
		return fmt.Errorf("log manager: %w", err)
	}

	progress := NewProgressState(repos, fmt.Sprintf("Executing '%s'", command), cfg.PageSize)
	stop := progress.start()

	results := runPool(ctx, repos, workers, func(ctx context.Context, r RepoInfo) CommandResult {
		progress.UpdateStatus(r.RelPath, statusProcessing, "")

		logFile, logErr := logManager.CreateLogFile(r.RelPath)
		if logErr != nil {
			cmdCtx, cancel := context.WithTimeout(ctx, gitCommandTimeout)
			defer cancel()
			var cmd *exec.Cmd
			if runtime.GOOS == "windows" {
				cmd = exec.CommandContext(cmdCtx, "cmd", "/c", command)
			} else {
				cmd = exec.CommandContext(cmdCtx, "sh", "-c", command)
			}
			cmd.Dir = r.Path
			output, cmdErr := cmd.CombinedOutput()
			st, msg := progressStatusFromErr(cmdErr)
			progress.UpdateStatus(r.RelPath, st, msg)
			return CommandResult{RelPath: r.RelPath, Output: string(output), Error: cmdErr}
		}
		cmdErr := executeShellCommandToFile(ctx, r.Path, logFile, command)
		_ = logFile.Close()
		st, msg := progressStatusFromErr(cmdErr)
		progress.UpdateStatus(r.RelPath, st, msg)
		return CommandResult{RelPath: r.RelPath, Error: cmdErr}
	})

	success, failed := 0, 0
	for _, res := range results {
		if res.Error != nil {
			failed++
		} else {
			success++
		}
	}

	stop()

	fmt.Println("\n" + StyleBold.Render("--- Summary ---"))
	fmt.Printf("Executed '%s' in %d repos: %s succeeded, %s failed\n",
		command, success+failed,
		StyleSuccess.Render(fmt.Sprintf("%d", success)),
		StyleFailed.Render(fmt.Sprintf("%d", failed)))

	if PromptViewLogs() {
		DisplayLogs(logManager, results)
	} else {
		fmt.Printf("\nLogs are available at: %s\n", logManager.GetTempDir())
		fmt.Println("You can review them later if needed.")
	}

	if failed > 0 {
		return errReposFailed
	}
	return nil
}
