package core

import (
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"
)

type SwitchResult struct {
	RelPath string
	Success bool
	Error   string
}

func switchBranches(root, target string, workers int) {
	repos, err := findGitRepos(root)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
	if len(repos) == 0 {
		fmt.Println("No repos found")
		return
	}
	fmt.Printf("Found %d repos, switching to %s with %d workers...\n", len(repos), target, workers)

	repoCh := make(chan RepoInfo, len(repos))
	resCh := make(chan SwitchResult, len(repos))

	var wg sync.WaitGroup
	var mu sync.Mutex
	processed := 0

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for r := range repoCh {
				res := processSingleRepo(r, target)
				mu.Lock()
				processed++
				fmt.Printf("[%d/%d] %s\n", processed, len(repos), res.RelPath)
				if !res.Success {
					fmt.Println("  Error:", res.Error)
				} else {
					fmt.Println("  OK")
				}
				mu.Unlock()
				resCh <- res
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

	var ok, fail int
	var failed []string
	for res := range resCh {
		if res.Success {
			ok++
		} else {
			fail++
			failed = append(failed, res.RelPath+" ("+res.Error+")")
		}
	}
	fmt.Println("\n--- Summary ---")
	fmt.Printf("Switched %d repos to %s\n", ok, target)
	if fail > 0 {
		fmt.Printf("Failed %d repos:\n", fail)
		sort.Strings(failed)
		for _, f := range failed {
			fmt.Println("  ", f)
		}
		os.Exit(1)
	}
}

func processSingleRepo(repo RepoInfo, targetBranch string) SwitchResult {
	// Check if local branch exists
	localCheck := exec.Command("git", "show-ref", "--verify", "--quiet", "refs/heads/"+targetBranch)
	localCheck.Dir = repo.Path
	branchExists := localCheck.Run() == nil

	if !branchExists {
		// Check if branch exists remotely
		remoteCheck := exec.Command("git", "ls-remote", "--exit-code", "--heads", "origin", targetBranch)
		remoteCheck.Dir = repo.Path
		if remoteCheck.Run() == nil {
			// Detect shallow repo
			out, err := exec.Command("git", "rev-parse", "--is-shallow-repository").Output()
			isShallow := err == nil && strings.TrimSpace(string(out)) == "true"

			// Fetch branch
			args := []string{"fetch", "origin"}
			if isShallow {
				args = append(args, "--depth=1")
			}
			args = append(args, targetBranch)

			fetchCmd := exec.Command("git", args...)
			fetchCmd.Dir = repo.Path
			if out, err := fetchCmd.CombinedOutput(); err != nil {
				return SwitchResult{
					RelPath: repo.RelPath,
					Success: false,
					Error:   "fetch failed: " + string(out),
				}
			}
			branchExists = true
		} else {
			return SwitchResult{RelPath: repo.RelPath, Success: false, Error: "branch not found"}
		}
	}

	// Try normal switch
	switchCmd := exec.Command("git", "switch", targetBranch)
	switchCmd.Dir = repo.Path
	if out, err := switchCmd.CombinedOutput(); err == nil {
		return SwitchResult{RelPath: repo.RelPath, Success: true}
	} else {
		// Try to create tracking branch from remote
		trackCmd := exec.Command("git", "switch", "-c", targetBranch, "--track", "origin/"+targetBranch)
		trackCmd.Dir = repo.Path
		if out2, err2 := trackCmd.CombinedOutput(); err2 == nil {
			return SwitchResult{RelPath: repo.RelPath, Success: true}
		} else {
			return SwitchResult{
				RelPath: repo.RelPath,
				Success: false,
				Error:   "switch failed: " + string(out) + string(out2),
			}
		}
	}
}
