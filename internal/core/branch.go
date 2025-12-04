package core

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"sort"
	"strings"
	"sync"
)

const (
	branchStateNoCommits = "no commits"
	branchStateDetached  = "detached"
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
	repos, err := findGitRepos(root, cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
	if len(repos) == 0 {
		fmt.Println("No repos found")
		return
	}

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
	repos, err := findGitRepos(root, cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
	if len(repos) == 0 {
		fmt.Println("No repos found")
		return
	}

	args := strings.Fields(command)
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "Error: Empty command")
		os.Exit(1)
	}

	fmt.Printf("Executing 'git %s' in %d repos with %d workers...\n", command, len(repos), workers)

	repoCh := make(chan RepoInfo, len(repos))
	resCh := make(chan CommandResult, len(repos))

	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for r := range repoCh {
				cmd := exec.Command("git", args...)
				cmd.Dir = r.Path
				output, err := cmd.CombinedOutput()

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
	fmt.Printf("Executed 'git %s' in %d repos: %d succeeded, %d failed\n", command, success+failed, success, failed)
	if failed > 0 {
		os.Exit(1)
	}
}

func executeShellInRepos(root, command string, workers int, cfg *Config) {
	fmt.Printf("Discovering repos in %s...\n", root)
	repos, err := findGitRepos(root, cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
	if len(repos) == 0 {
		fmt.Println("No repos found")
		return
	}

	if strings.TrimSpace(command) == "" {
		fmt.Fprintln(os.Stderr, "Error: Empty command")
		os.Exit(1)
	}

	fmt.Printf("Executing '%s' in %d repos with %d workers...\n", command, len(repos), workers)

	repoCh := make(chan RepoInfo, len(repos))
	resCh := make(chan CommandResult, len(repos))

	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for r := range repoCh {
				var cmd *exec.Cmd
				if runtime.GOOS == "windows" {
					cmd = exec.Command("cmd", "/c", command)
				} else {
					cmd = exec.Command("sh", "-c", command)
				}
				cmd.Dir = r.Path
				output, err := cmd.CombinedOutput()

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
