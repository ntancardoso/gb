package core

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

type SwitchResult struct {
	RelPath string
	Success bool
	Skipped bool
	Error   string
}

func switchBranches(ctx context.Context, root, target string, workers int, cfg *Config) error {
	repos, total := discoverRepos(root, workers, cfg, false)
	if repos == nil {
		return nil
	}

	fmt.Println(StyleInfo.Render(fmt.Sprintf("Found %d repos (filtered from %d discovered), switching to %s with %d workers...", len(repos), total, target, min(workers, len(repos)))))

	logManager, err := NewLogManager()
	if err != nil {
		return fmt.Errorf("log manager: %w", err)
	}

	progress := NewProgressState(repos, "Switching branches", cfg.PageSize)
	stop := progress.start()

	results := runPool(ctx, repos, workers, func(_ context.Context, r RepoInfo) SwitchResult {
		progress.UpdateStatus(r.RelPath, statusProcessing, "")

		logFile, _ := logManager.CreateLogFile(r.RelPath)
		res := processSingleRepo(r, target, cfg.Remote, logFile)
		if logFile != nil {
			_ = logFile.Close()
		}

		switch {
		case res.Skipped:
			progress.UpdateStatus(r.RelPath, statusSkipped, res.Error)
		case res.Success:
			progress.UpdateStatus(r.RelPath, statusCompleted, "")
		default:
			progress.UpdateStatus(r.RelPath, statusFailed, res.Error)
		}
		return res
	})

	var ok, fail, skip int
	for _, res := range results {
		switch {
		case res.Skipped:
			skip++
		case res.Success:
			ok++
		default:
			fail++
		}
	}

	stop()

	fmt.Println("\n" + StyleBold.Render("--- Summary ---"))
	fmt.Printf("Switched %s repos to %s, %s skipped (branch in worktree), %s failed\n",
		StyleSuccess.Render(fmt.Sprintf("%d", ok)),
		target,
		StyleSkipped.Render(fmt.Sprintf("%d", skip)),
		StyleFailed.Render(fmt.Sprintf("%d", fail)))

	if PromptViewLogs() {
		DisplaySwitchLogs(logManager, results)
	} else {
		fmt.Printf("\nLogs are available at: %s\n", logManager.GetTempDir())
		fmt.Println("You can review them later if needed.")
	}

	if fail > 0 {
		return errReposFailed
	}
	return nil
}

func isBranchLockedInWorktree(repoPath, targetBranch string) bool {
	cmd := exec.Command("git", "worktree", "list", "--porcelain")
	cmd.Dir = repoPath
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	normalized := strings.ReplaceAll(string(out), "\r\n", "\n")
	blocks := strings.Split(strings.TrimSpace(normalized), "\n\n")
	if len(blocks) <= 1 {
		return false
	}
	for _, block := range blocks[1:] {
		for _, line := range strings.Split(block, "\n") {
			if strings.TrimSpace(line) == "branch refs/heads/"+targetBranch {
				return true
			}
		}
	}
	return false
}

func processSingleRepo(repo RepoInfo, targetBranch, remote string, logFile *os.File) SwitchResult {
	log := func(format string, args ...any) {
		if logFile != nil {
			_, _ = fmt.Fprintf(logFile, format+"\n", args...)
		}
	}

	log("=== Processing %s ===", repo.RelPath)
	log("Target branch: %s", targetBranch)

	if isBranchLockedInWorktree(repo.Path, targetBranch) {
		log("Target branch is locked in a worktree, skipping")
		return SwitchResult{RelPath: repo.RelPath, Skipped: true, Error: "branch locked in worktree"}
	}

	localCheck := exec.Command("git", "show-ref", "--verify", "--quiet", "refs/heads/"+targetBranch)
	localCheck.Dir = repo.Path
	branchExists := localCheck.Run() == nil

	log("Local branch exists: %v", branchExists)

	if !branchExists {
		log("Checking remote for branch...")
		remoteCheck := exec.Command("git", "ls-remote", "--exit-code", "--heads", remote, targetBranch)
		remoteCheck.Dir = repo.Path
		if remoteCheck.Run() == nil {
			checkCmd := exec.Command("git", "rev-parse", "--is-shallow-repository")
			checkCmd.Dir = repo.Path
			out, err := checkCmd.Output()
			isShallow := err == nil && strings.TrimSpace(string(out)) == "true"

			args := []string{"fetch", remote}
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
				return SwitchResult{RelPath: repo.RelPath, Success: false, Error: "fetch failed"}
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
	log("Executing: git switch -c %s --track %s/%s", targetBranch, remote, targetBranch)
	trackCmd := exec.Command("git", "switch", "-c", targetBranch, "--track", remote+"/"+targetBranch)
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
	return SwitchResult{RelPath: repo.RelPath, Success: false, Error: "switch failed"}
}
