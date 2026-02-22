package core

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// ResetResult holds the outcome of a single repo reset/rebase operation.
type ResetResult struct {
	RelPath    string
	Success    bool
	Skipped    bool
	SkipReason string
	Error      string
	Warning    string
}

// repoPreflightInfo holds dirty-state info gathered during pre-flight scan.
type repoPreflightInfo struct {
	RelPath     string
	Path        string
	DirtyStatus string
}

func syncBranch(root, branch, mode string, workers int, cfg *Config) {
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

	// Destructive operations require interactive TTY and explicit confirmation.
	if mode == "hard" || mode == "rebase" {
		fileInfo, statErr := os.Stdin.Stat()
		if statErr != nil || (fileInfo.Mode()&os.ModeCharDevice) == 0 {
			fmt.Fprintln(os.Stderr, "Error: stdin is not a terminal. Destructive operations require interactive confirmation.")
			fmt.Fprintln(os.Stderr, "Use -rs (soft reset) for non-interactive use.")
			os.Exit(1)
		}

		dirtyRepos := preflightScan(repos, workers)
		if !PromptConfirmDestructive(operationDescription(mode, branch), len(repos), dirtyRepos) {
			fmt.Println("Aborted.")
			return
		}
	}

	opDesc := operationDescription(mode, branch)
	fmt.Printf("Found %d repos (filtered from %d discovered), running '%s' with %d workers...\n",
		len(repos), len(allRepos), opDesc, workers)

	logManager, err := NewLogManager()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating log manager: %s\n", err)
		os.Exit(1)
	}

	progress := NewProgressState(repos, opDesc, cfg.PageSize)
	progress.render()
	progress.StartInput()

	repoCh := make(chan RepoInfo, len(repos))
	resCh := make(chan ResetResult, len(repos))

	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for r := range repoCh {
				progress.UpdateStatus(r.RelPath, statusProcessing, "")

				logFile, logErr := logManager.CreateLogFile(r.RelPath)
				if logErr != nil {
					logFile = nil
				}

				res := processSingleReset(r, branch, mode, logFile)

				if logFile != nil {
					_ = logFile.Close()
				}

				switch {
				case res.Skipped:
					progress.UpdateStatus(r.RelPath, statusCompleted, "skipped: "+res.SkipReason)
				case res.Success:
					progress.UpdateStatus(r.RelPath, statusCompleted, "")
				default:
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

	var results []ResetResult
	var succeeded, failed, skipped int
	skipReasons := make(map[string]int)

	for res := range resCh {
		results = append(results, res)
		switch {
		case res.Skipped:
			skipped++
			skipReasons[res.SkipReason]++
		case res.Success:
			succeeded++
		default:
			failed++
		}
	}

	progress.RenderFinal()
	progress.StopInput()

	fmt.Printf("\n--- Summary ---\n")
	fmt.Printf("Ran '%s' across %d repos:\n", opDesc, len(repos))
	fmt.Printf("  %d succeeded\n", succeeded)
	fmt.Printf("  %d failed\n", failed)
	if skipped > 0 {
		// Sort for deterministic output.
		reasons := make([]string, 0, len(skipReasons))
		for reason := range skipReasons {
			reasons = append(reasons, reason)
		}
		sort.Strings(reasons)
		parts := make([]string, 0, len(reasons))
		for _, reason := range reasons {
			parts = append(parts, fmt.Sprintf("%s: %d", reason, skipReasons[reason]))
		}
		fmt.Printf("  %d skipped (%s)\n", skipped, strings.Join(parts, ", "))
	}

	if PromptViewLogs() {
		DisplayResetLogs(logManager, results)
	} else {
		fmt.Printf("\nLogs are available at: %s\n", logManager.GetTempDir())
		fmt.Println("You can review them later if needed.")
	}

	if failed > 0 {
		os.Exit(1)
	}
}

func operationDescription(mode, branch string) string {
	switch mode {
	case "soft":
		return fmt.Sprintf("git reset --soft origin/%s", branch)
	case "hard":
		return fmt.Sprintf("git reset --hard origin/%s", branch)
	case "rebase":
		return fmt.Sprintf("git rebase origin/%s", branch)
	}
	return "unknown operation"
}

// preflightScan concurrently checks all repos for dirty working trees.
func preflightScan(repos []RepoInfo, workers int) []repoPreflightInfo {
	repoCh := make(chan RepoInfo, len(repos))
	resCh := make(chan repoPreflightInfo, len(repos))

	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for r := range repoCh {
				if status := getDirtyStatus(r.Path); status != "" {
					resCh <- repoPreflightInfo{RelPath: r.RelPath, Path: r.Path, DirtyStatus: status}
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

	var dirty []repoPreflightInfo
	for info := range resCh {
		dirty = append(dirty, info)
	}
	sort.Slice(dirty, func(i, j int) bool { return dirty[i].RelPath < dirty[j].RelPath })
	return dirty
}

// getDirtyStatus returns a human-readable description of dirty state, or "" if clean.
func getDirtyStatus(dir string) string {
	cmd := exec.Command("git", "status", "--porcelain")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	trimmed := strings.TrimSpace(string(out))
	if trimmed == "" {
		return ""
	}

	hasStaged, hasUnstaged := false, false
	for _, line := range strings.Split(trimmed, "\n") {
		if len(line) < 2 {
			continue
		}
		if line[0] != ' ' && line[0] != '?' {
			hasStaged = true
		}
		if line[1] != ' ' {
			hasUnstaged = true
		}
	}

	switch {
	case hasStaged && hasUnstaged:
		return "staged + unstaged changes"
	case hasStaged:
		return "staged changes"
	case hasUnstaged:
		return "unstaged changes"
	}
	return "changes"
}

func processSingleReset(repo RepoInfo, branch, mode string, logFile *os.File) ResetResult {
	log := func(format string, args ...interface{}) {
		if logFile != nil {
			_, _ = fmt.Fprintf(logFile, format+"\n", args...)
		}
	}

	log("=== Processing %s ===", repo.RelPath)
	log("Target branch: %s, Mode: %s", branch, mode)

	if !checkOriginExists(repo.Path) {
		log("Skipping: no origin remote")
		return ResetResult{RelPath: repo.RelPath, Skipped: true, SkipReason: "no origin remote"}
	}

	if !checkHasCommits(repo.Path) {
		log("Skipping: no commits")
		return ResetResult{RelPath: repo.RelPath, Skipped: true, SkipReason: "no commits"}
	}

	if checkDetachedHEAD(repo.Path) {
		log("Skipping: detached HEAD")
		return ResetResult{RelPath: repo.RelPath, Skipped: true, SkipReason: "detached HEAD"}
	}

	found, netErr := checkBranchOnOrigin(repo.Path, branch)
	if netErr != nil {
		log("Error checking remote branch: %v", netErr)
		return ResetResult{RelPath: repo.RelPath, Success: false, Error: fmt.Sprintf("network error: %v", netErr)}
	}
	if !found {
		log("Skipping: branch not on origin")
		return ResetResult{RelPath: repo.RelPath, Skipped: true, SkipReason: "branch not on origin"}
	}

	currentBranch, err := getCurrentBranch(repo.Path)
	if err != nil {
		log("Error getting current branch: %v", err)
		return ResetResult{RelPath: repo.RelPath, Success: false, Error: "failed to get current branch"}
	}

	if currentBranch != branch {
		log("Switching from %s to %s", currentBranch, branch)
		if switchErr := switchToTargetBranch(repo, branch, logFile); switchErr != nil {
			log("Switch failed: %v", switchErr)
			return ResetResult{RelPath: repo.RelPath, Success: false, Error: fmt.Sprintf("switch to %s failed", branch)}
		}
	} else {
		// Already on target branch — fetch to update the origin/<branch> ref.
		log("Already on %s, fetching to update origin/%s ref", branch, branch)
		if fetchErr := fetchBranchFromOrigin(repo.Path, branch, logFile); fetchErr != nil {
			log("Fetch failed: %v", fetchErr)
			return ResetResult{RelPath: repo.RelPath, Success: false, Error: "fetch failed"}
		}
	}

	if checkAlreadyAtTarget(repo.Path, branch) {
		log("Skipping: already up to date")
		return ResetResult{RelPath: repo.RelPath, Skipped: true, SkipReason: "already up to date"}
	}

	switch mode {
	case "hard":
		return doHardReset(repo, branch, logFile, log)
	case "soft":
		return doSoftReset(repo, branch, logFile, log)
	case "rebase":
		return doRebase(repo, branch, logFile, log)
	}
	return ResetResult{RelPath: repo.RelPath, Success: false, Error: "unknown mode"}
}

func doHardReset(repo RepoInfo, branch string, logFile *os.File, log func(string, ...interface{})) ResetResult {
	if inProgress, opName := checkMidOperation(repo.Path); inProgress {
		log("Skipping: mid-%s operation in progress", opName)
		return ResetResult{RelPath: repo.RelPath, Skipped: true, SkipReason: fmt.Sprintf("mid-%s in progress", opName)}
	}

	log("Executing: git reset --hard origin/%s", branch)
	cmd := exec.Command("git", "reset", "--hard", "origin/"+branch)
	cmd.Dir = repo.Path
	if logFile != nil {
		cmd.Stdout = logFile
		cmd.Stderr = logFile
	}
	if err := cmd.Run(); err != nil {
		log("Hard reset failed: %v", err)
		return ResetResult{RelPath: repo.RelPath, Success: false, Error: "reset --hard failed"}
	}
	log("Hard reset completed successfully")
	return ResetResult{RelPath: repo.RelPath, Success: true}
}

func doSoftReset(repo RepoInfo, branch string, logFile *os.File, log func(string, ...interface{})) ResetResult {
	warning := ""
	stagedCheck := exec.Command("git", "diff", "--cached", "--quiet")
	stagedCheck.Dir = repo.Path
	if stagedCheck.Run() != nil {
		warning = "had staged changes before reset"
		log("Warning: staged changes exist; soft reset will merge staged state")
	}

	log("Executing: git reset --soft origin/%s", branch)
	cmd := exec.Command("git", "reset", "--soft", "origin/"+branch)
	cmd.Dir = repo.Path
	if logFile != nil {
		cmd.Stdout = logFile
		cmd.Stderr = logFile
	}
	if err := cmd.Run(); err != nil {
		log("Soft reset failed: %v", err)
		return ResetResult{RelPath: repo.RelPath, Success: false, Error: "reset --soft failed"}
	}
	log("Soft reset completed successfully")
	return ResetResult{RelPath: repo.RelPath, Success: true, Warning: warning}
}

func doRebase(repo RepoInfo, branch string, logFile *os.File, log func(string, ...interface{})) ResetResult {
	if checkRebaseInProgress(repo.Path) {
		log("Skipping: rebase already in progress")
		return ResetResult{RelPath: repo.RelPath, Skipped: true, SkipReason: "rebase already in progress"}
	}

	if status := getDirtyStatus(repo.Path); status != "" {
		log("Failed: working tree must be clean for rebase (%s)", status)
		return ResetResult{RelPath: repo.RelPath, Success: false, Error: "working tree must be clean"}
	}

	log("Executing: git rebase origin/%s", branch)
	cmd := exec.Command("git", "rebase", "origin/"+branch)
	cmd.Dir = repo.Path
	if logFile != nil {
		cmd.Stdout = logFile
		cmd.Stderr = logFile
	}
	if err := cmd.Run(); err != nil {
		log("Rebase failed: %v, aborting...", err)
		abortCmd := exec.Command("git", "rebase", "--abort")
		abortCmd.Dir = repo.Path
		if logFile != nil {
			abortCmd.Stdout = logFile
			abortCmd.Stderr = logFile
		}
		if abortErr := abortCmd.Run(); abortErr != nil {
			log("Rebase abort failed: %v", abortErr)
			return ResetResult{RelPath: repo.RelPath, Success: false, Error: "conflict during rebase; abort failed — manual cleanup required"}
		}
		log("Rebase aborted")
		return ResetResult{RelPath: repo.RelPath, Success: false, Error: "conflict during rebase, aborted"}
	}
	log("Rebase completed successfully")
	return ResetResult{RelPath: repo.RelPath, Success: true}
}

// --- helpers ---

func checkOriginExists(dir string) bool {
	cmd := exec.Command("git", "remote", "get-url", "origin")
	cmd.Dir = dir
	return cmd.Run() == nil
}

func checkHasCommits(dir string) bool {
	cmd := exec.Command("git", "rev-parse", "--verify", "HEAD")
	cmd.Dir = dir
	return cmd.Run() == nil
}

func checkDetachedHEAD(dir string) bool {
	cmd := exec.Command("git", "branch", "--show-current")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return true
	}
	return strings.TrimSpace(string(out)) == ""
}

func checkBranchOnOrigin(dir, branch string) (bool, error) {
	cmd := exec.Command("git", "ls-remote", "--exit-code", "--heads", "origin", branch)
	cmd.Dir = dir
	err := cmd.Run()
	if err == nil {
		return true, nil
	}
	if exitErr, ok := err.(*exec.ExitError); ok {
		if exitErr.ExitCode() == 2 {
			return false, nil
		}
		return false, fmt.Errorf("ls-remote failed with exit code %d", exitErr.ExitCode())
	}
	return false, err
}

func getCurrentBranch(dir string) (string, error) {
	cmd := exec.Command("git", "branch", "--show-current")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func checkMidOperation(dir string) (bool, string) {
	checks := []struct {
		file string
		name string
	}{
		{".git/MERGE_HEAD", "merge"},
		{".git/CHERRY_PICK_HEAD", "cherry-pick"},
		{".git/REVERT_HEAD", "revert"},
	}
	for _, check := range checks {
		if _, err := os.Stat(filepath.Join(dir, check.file)); err == nil {
			return true, check.name
		}
	}
	return false, ""
}

func checkRebaseInProgress(dir string) bool {
	for _, p := range []string{".git/rebase-merge", ".git/rebase-apply"} {
		if _, err := os.Stat(filepath.Join(dir, p)); err == nil {
			return true
		}
	}
	return false
}

func checkAlreadyAtTarget(dir, branch string) bool {
	headCmd := exec.Command("git", "rev-parse", "HEAD")
	headCmd.Dir = dir
	headOut, err := headCmd.Output()
	if err != nil {
		return false
	}

	originCmd := exec.Command("git", "rev-parse", "origin/"+branch)
	originCmd.Dir = dir
	originOut, err := originCmd.Output()
	if err != nil {
		return false
	}

	return strings.TrimSpace(string(headOut)) == strings.TrimSpace(string(originOut))
}

// fetchBranchFromOrigin fetches branch from origin, respecting shallow repos.
func fetchBranchFromOrigin(dir, branch string, logFile *os.File) error {
	checkCmd := exec.Command("git", "rev-parse", "--is-shallow-repository")
	checkCmd.Dir = dir
	shallowOut, shallowErr := checkCmd.Output()
	isShallow := shallowErr == nil && strings.TrimSpace(string(shallowOut)) == "true"

	args := []string{"fetch", "origin"}
	if isShallow {
		args = append(args, "--depth=1")
	}
	args = append(args, branch)

	fetchCmd := exec.Command("git", args...)
	fetchCmd.Dir = dir
	if logFile != nil {
		fetchCmd.Stdout = logFile
		fetchCmd.Stderr = logFile
	}
	if err := fetchCmd.Run(); err != nil {
		return fmt.Errorf("fetch failed")
	}
	return nil
}

// switchToTargetBranch switches the repo to branch, fetching from origin if needed.
// Mirrors the logic in processSingleRepo (switch.go).
func switchToTargetBranch(repo RepoInfo, branch string, logFile *os.File) error {
	localCheck := exec.Command("git", "show-ref", "--verify", "--quiet", "refs/heads/"+branch)
	localCheck.Dir = repo.Path
	branchExists := localCheck.Run() == nil

	if !branchExists {
		if err := fetchBranchFromOrigin(repo.Path, branch, logFile); err != nil {
			return err
		}
	}

	switchCmd := exec.Command("git", "switch", branch)
	switchCmd.Dir = repo.Path
	if logFile != nil {
		switchCmd.Stdout = logFile
		switchCmd.Stderr = logFile
	}
	if switchCmd.Run() == nil {
		return nil
	}

	trackCmd := exec.Command("git", "switch", "-c", branch, "--track", "origin/"+branch)
	trackCmd.Dir = repo.Path
	if logFile != nil {
		trackCmd.Stdout = logFile
		trackCmd.Stderr = logFile
	}
	if trackCmd.Run() == nil {
		return nil
	}

	return fmt.Errorf("switch failed")
}
