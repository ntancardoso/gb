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

func executeGitCommandWithRetry(ctx context.Context, dir string, args ...string) ([]byte, error, int) {
	var lastErr error
	var output []byte

	for attempt := 0; attempt < maxRetries; attempt++ {
		cmdCtx, cancel := context.WithTimeout(ctx, gitCommandTimeout)
		cmd := exec.CommandContext(cmdCtx, "git", args...)
		cmd.Dir = dir

		output, lastErr = cmd.CombinedOutput()
		cancel()

		if lastErr == nil {
			return output, nil, attempt
		}

		if cmdCtx.Err() != nil {
			if attempt < maxRetries-1 {
				time.Sleep(retryDelay)
				continue
			}
		} else {
			return output, lastErr, attempt
		}
	}

	return output, lastErr, maxRetries - 1
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

	fmt.Printf("Executing 'git %s' in %d repos (filtered from %d discovered) with %d workers...\n", command, len(repos), len(allRepos), workers)

	repoCh := make(chan RepoInfo, len(repos))
	resCh := make(chan CommandResult, len(repos))

	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for r := range repoCh {
				output, err, retries := executeGitCommandWithRetry(context.Background(), r.Path, args...)

				resCh <- CommandResult{
					RelPath: r.RelPath,
					Output:  string(output),
					Error:   err,
					Retries: retries,
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

	success, failed := 0, 0
	for res := range resCh {
		retryInfo := ""
		if res.Retries > 0 {
			retryInfo = fmt.Sprintf(" (retried %d time(s))", res.Retries)
		}

		if res.Error != nil {
			fmt.Printf("❌ %s%s:\n%s\n%s\n", res.RelPath, retryInfo, res.Output, res.Error)
			failed++
		} else {
			if strings.TrimSpace(res.Output) != "" {
				fmt.Printf("✅ %s%s:\n%s\n", res.RelPath, retryInfo, res.Output)
			} else {
				fmt.Printf("✅ %s%s: OK\n", res.RelPath, retryInfo)
			}
			success++
		}
	}

	fmt.Printf("\n--- Summary ---\n")
	fmt.Printf("Executed 'git %s' in %d repos: %d succeeded, %d failed\n", command, success+failed, success, failed)
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

	fmt.Printf("Executing '%s' in %d repos (filtered from %d discovered) with %d workers...\n", command, len(repos), len(allRepos), workers)

	repoCh := make(chan RepoInfo, len(repos))
	resCh := make(chan CommandResult, len(repos))

	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for r := range repoCh {
				ctx, cancel := context.WithTimeout(context.Background(), gitCommandTimeout)
				var cmd *exec.Cmd
				if runtime.GOOS == "windows" {
					cmd = exec.CommandContext(ctx, "cmd", "/c", command)
				} else {
					cmd = exec.CommandContext(ctx, "sh", "-c", command)
				}
				cmd.Dir = r.Path
				output, err := cmd.CombinedOutput()
				cancel()

				resCh <- CommandResult{
					RelPath: r.RelPath,
					Output:  string(output),
					Error:   err,
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

	success, failed := 0, 0
	for res := range resCh {
		if res.Error != nil {
			fmt.Printf("❌ %s:\n%s\n%s\n", res.RelPath, res.Output, res.Error)
			failed++
		} else {
			if strings.TrimSpace(res.Output) != "" {
				fmt.Printf("✅ %s:\n%s\n", res.RelPath, res.Output)
			} else {
				fmt.Printf("✅ %s: OK\n", res.RelPath)
			}
			success++
		}
	}

	fmt.Printf("\n--- Summary ---\n")
	fmt.Printf("Executed '%s' in %d repos: %d succeeded, %d failed\n", command, success+failed, success, failed)
	if failed > 0 {
		os.Exit(1)
	}
}
