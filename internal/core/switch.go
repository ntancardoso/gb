package core

import (
	"fmt"
	"os"
	"os/exec"
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

	logManager, err := NewLogManager()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating log manager: %s\n", err)
		os.Exit(1)
	}

	progress := NewProgressState(repos, "Switching branches", cfg.PageSize)
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

				logFile, err := logManager.CreateLogFile(r.RelPath)
				if err != nil {
					logFile = nil
				}

				res := processSingleRepo(r, target, logFile)

				if logFile != nil {
					_ = logFile.Close()
				}

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

	var results []SwitchResult
	var ok, fail int
	for res := range resCh {
		results = append(results, res)
		if res.Success {
			ok++
		} else {
			fail++
		}
	}

	progress.RenderFinal()

	fmt.Printf("\n--- Summary ---\n")
	fmt.Printf("Switched %d repos to %s, %d failed\n", ok, target, fail)

	if PromptViewLogs() {
		DisplaySwitchLogs(logManager, results)
	} else {
		fmt.Printf("\nLogs are available at: %s\n", logManager.GetTempDir())
		fmt.Println("You can review them later if needed.")
	}

	if fail > 0 {
		os.Exit(1)
	}
}

func processSingleRepo(repo RepoInfo, targetBranch string, logFile *os.File) SwitchResult {
	log := func(format string, args ...interface{}) {
		if logFile != nil {
			_, _ = fmt.Fprintf(logFile, format+"\n", args...)
		}
	}

	log("=== Processing %s ===", repo.RelPath)
	log("Target branch: %s", targetBranch)

	localCheck := exec.Command("git", "show-ref", "--verify", "--quiet", "refs/heads/"+targetBranch)
	localCheck.Dir = repo.Path
	branchExists := localCheck.Run() == nil

	log("Local branch exists: %v", branchExists)

	if !branchExists {
		log("Checking remote for branch...")
		remoteCheck := exec.Command("git", "ls-remote", "--exit-code", "--heads", "origin", targetBranch)
		remoteCheck.Dir = repo.Path
		if remoteCheck.Run() == nil {
			checkCmd := exec.Command("git", "rev-parse", "--is-shallow-repository")
			checkCmd.Dir = repo.Path
			out, err := checkCmd.Output()
			isShallow := err == nil && strings.TrimSpace(string(out)) == "true"

			log("Is shallow repository: %v", isShallow)

			args := []string{"fetch", "origin"}
			if isShallow {
				args = append(args, "--depth=1")
			}
			args = append(args, targetBranch)

			log("Executing: git %s", strings.Join(args, " "))
			fetchCmd := exec.Command("git", args...)
			fetchCmd.Dir = repo.Path
			if logFile != nil {
				fetchCmd.Stdout = logFile
				fetchCmd.Stderr = logFile
			}
			if err := fetchCmd.Run(); err != nil {
				log("Fetch failed: %v", err)
				return SwitchResult{
					RelPath: repo.RelPath,
					Success: false,
					Error:   "fetch failed",
				}
			}
			log("Fetch completed successfully")
		} else {
			log("Branch not found on remote")
			return SwitchResult{RelPath: repo.RelPath, Success: false, Error: "branch not found"}
		}
	}

	log("Executing: git switch %s", targetBranch)
	switchCmd := exec.Command("git", "switch", targetBranch)
	switchCmd.Dir = repo.Path
	if logFile != nil {
		switchCmd.Stdout = logFile
		switchCmd.Stderr = logFile
	}
	if err := switchCmd.Run(); err == nil {
		log("Switch completed successfully")
		return SwitchResult{RelPath: repo.RelPath, Success: true}
	}

	log("Switch failed, trying to create tracking branch...")
	log("Executing: git switch -c %s --track origin/%s", targetBranch, targetBranch)
	trackCmd := exec.Command("git", "switch", "-c", targetBranch, "--track", "origin/"+targetBranch)
	trackCmd.Dir = repo.Path
	if logFile != nil {
		trackCmd.Stdout = logFile
		trackCmd.Stderr = logFile
	}
	if err := trackCmd.Run(); err == nil {
		log("Created tracking branch successfully")
		return SwitchResult{RelPath: repo.RelPath, Success: true}
	}

	log("All switch attempts failed")
	return SwitchResult{
		RelPath: repo.RelPath,
		Success: false,
		Error:   "switch failed",
	}
}
