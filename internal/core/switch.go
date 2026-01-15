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

func switchBranches(root, target string, workers int, cfg *Config) {
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

	fmt.Printf("Found %d repos (filtered from %d discovered), switching to %s with %d workers...\n", len(repos), len(allRepos), target, workers)

	progress := NewProgressState(repos, "Switching branches")
	progress.render()
	progress.StartInput()
	defer progress.StopInput()

	repoCh := make(chan RepoInfo, len(repos))
	resCh := make(chan SwitchResult, len(repos))

	var wg sync.WaitGroup

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for r := range repoCh {
				progress.UpdateStatus(r.RelPath, statusProcessing, "")
				res := processSingleRepo(r, target)

				if res.Success {
					progress.UpdateStatus(r.RelPath, statusCompleted, "")
				} else {
					progress.UpdateStatus(r.RelPath, statusFailed, res.Error)
				}

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

	progress.RenderFinal()

	fmt.Printf("\n--- Summary ---\n")
	fmt.Printf("Switched %d repos to %s\n", ok, target)
	if fail > 0 {
		fmt.Printf("Failed %d repos:\n", fail)
		sort.Strings(failed)
		for _, f := range failed {
			fmt.Printf("  %s\n", f)
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
		remoteCheck := exec.Command("git", "ls-remote", "--exit-code", "--heads", "origin", targetBranch)
		remoteCheck.Dir = repo.Path
		if remoteCheck.Run() == nil {
			out, err := exec.Command("git", "rev-parse", "--is-shallow-repository").Output()
			isShallow := err == nil && strings.TrimSpace(string(out)) == "true"

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
		} else {
			return SwitchResult{RelPath: repo.RelPath, Success: false, Error: "branch not found"}
		}
	}

	switchCmd := exec.Command("git", "switch", targetBranch)
	switchCmd.Dir = repo.Path
	if out, err := switchCmd.CombinedOutput(); err == nil {
		return SwitchResult{RelPath: repo.RelPath, Success: true}
	} else {
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
