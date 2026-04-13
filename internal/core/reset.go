package core

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

type ResetResult struct {
	RelPath    string
	Success    bool
	Skipped    bool
	SkipReason string
	Error      string
	Warning    string
}

type repoPreflightInfo struct {
	RelPath     string
	Path        string
	DirtyStatus string
}

func syncBranch(ctx context.Context, root, branch, mode string, workers int, cfg *Config) error {
	remote := cfg.Remote
	repos, total := discoverRepos(root, workers, cfg, false)
	if repos == nil {
		return nil
	}

	if mode == "hard" || mode == "rebase" {
		fileInfo, statErr := os.Stdin.Stat()
		if statErr != nil || (fileInfo.Mode()&os.ModeCharDevice) == 0 {
			return fmt.Errorf("stdin is not a terminal; destructive operations require interactive confirmation — use -rs for non-interactive use")
		}

		dirtyRepos := preflightScan(ctx, repos, workers)
		if !PromptConfirmDestructive(operationDescription(mode, branch, remote), len(repos), dirtyRepos) {
			fmt.Println("Aborted.")
			return nil
		}
	}

	opDesc := operationDescription(mode, branch, remote)
	fmt.Println(StyleInfo.Render(fmt.Sprintf("Found %d repos (filtered from %d discovered), running '%s' with %d workers...",
		len(repos), total, opDesc, min(workers, len(repos)))))

	logManager, err := NewLogManager()
	if err != nil {
		return fmt.Errorf("log manager: %w", err)
	}

	progress := NewProgressState(repos, opDesc, cfg.PageSize)
	stop := progress.start()

	results := runPool(ctx, repos, workers, func(_ context.Context, r RepoInfo) ResetResult {
		progress.UpdateStatus(r.RelPath, statusProcessing, "")

		logFile, _ := logManager.CreateLogFile(r.RelPath)
		res := processSingleReset(r, branch, mode, remote, logFile)
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
		return res
	})

	var succeeded, failed, skipped int
	skipReasons := make(map[string]int)
	for _, res := range results {
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

	stop()

	fmt.Println("\n" + StyleBold.Render("--- Summary ---"))
	fmt.Printf("Ran '%s' across %d repos:\n", opDesc, len(repos))
	fmt.Printf("  %s succeeded\n", StyleSuccess.Render(fmt.Sprintf("%d", succeeded)))
	fmt.Printf("  %s failed\n", StyleFailed.Render(fmt.Sprintf("%d", failed)))
	if skipped > 0 {
		reasons := make([]string, 0, len(skipReasons))
		for reason := range skipReasons {
			reasons = append(reasons, reason)
		}
		sort.Strings(reasons)
		parts := make([]string, 0, len(reasons))
		for _, reason := range reasons {
			parts = append(parts, fmt.Sprintf("%s: %d", reason, skipReasons[reason]))
		}
		fmt.Printf("  %s skipped (%s)\n", StyleSkipped.Render(fmt.Sprintf("%d", skipped)), strings.Join(parts, ", "))
	}

	if PromptViewLogs() {
		DisplayResetLogs(logManager, results)
	} else {
		fmt.Printf("\nLogs are available at: %s\n", logManager.GetTempDir())
		fmt.Println("You can review them later if needed.")
	}

	if failed > 0 {
		return errReposFailed
	}
	return nil
}

func operationDescription(mode, branch, remote string) string {
	switch mode {
	case "soft":
		return fmt.Sprintf("git reset --soft %s/%s", remote, branch)
	case "hard":
		return fmt.Sprintf("git reset --hard %s/%s", remote, branch)
	case "rebase":
		return fmt.Sprintf("git rebase %s/%s", remote, branch)
	}
	return "unknown operation"
}

func preflightScan(ctx context.Context, repos []RepoInfo, workers int) []repoPreflightInfo {
	results := runPool(ctx, repos, workers, func(_ context.Context, r RepoInfo) repoPreflightInfo {
		return repoPreflightInfo{RelPath: r.RelPath, Path: r.Path, DirtyStatus: getDirtyStatus(r.Path)}
	})

	dirty := make([]repoPreflightInfo, 0, len(results))
	for _, info := range results {
		if info.DirtyStatus != "" {
			dirty = append(dirty, info)
		}
	}
	sort.Slice(dirty, func(i, j int) bool { return dirty[i].RelPath < dirty[j].RelPath })
	return dirty
}

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

func processSingleReset(repo RepoInfo, branch, mode, remote string, logFile *os.File) ResetResult {
	log := func(format string, args ...any) {
		if logFile != nil {
			_, _ = fmt.Fprintf(logFile, format+"\n", args...)
		}
	}

	remote, branch = resolveRemoteAndBranch(repo.Path, branch, remote)
	log("=== Processing %s ===", repo.RelPath)
	log("Target branch: %s, Mode: %s, Remote: %s", branch, mode, remote)

	if !checkRemoteExists(repo.Path, remote) {
		log("Skipping: no %s remote", remote)
		return ResetResult{RelPath: repo.RelPath, Skipped: true, SkipReason: "no " + remote + " remote"}
	}

	if !checkHasCommits(repo.Path) {
		log("Skipping: no commits")
		return ResetResult{RelPath: repo.RelPath, Skipped: true, SkipReason: "no commits"}
	}

	if checkDetachedHEAD(repo.Path) {
		log("Skipping: detached HEAD")
		return ResetResult{RelPath: repo.RelPath, Skipped: true, SkipReason: "detached HEAD"}
	}

	found, netErr := checkBranchOnRemote(repo.Path, branch, remote)
	if netErr != nil {
		log("Error checking remote branch: %v", netErr)
		return ResetResult{RelPath: repo.RelPath, Success: false, Error: fmt.Sprintf("network error: %v", netErr)}
	}
	if !found {
		log("Skipping: branch not on %s", remote)
		return ResetResult{RelPath: repo.RelPath, Skipped: true, SkipReason: "branch not on " + remote}
	}

	log("Fetching to update %s/%s ref", remote, branch)
	if fetchErr := fetchBranchFromRemote(repo.Path, branch, remote, logFile); fetchErr != nil {
		log("Fetch failed: %v", fetchErr)
		return ResetResult{RelPath: repo.RelPath, Success: false, Error: "fetch failed"}
	}

	if mode == "soft" && checkAlreadyAtTarget(repo.Path, branch, remote) {
		log("Skipping: already up to date")
		return ResetResult{RelPath: repo.RelPath, Skipped: true, SkipReason: "already up to date"}
	}

	switch mode {
	case "hard":
		return doHardReset(repo, branch, remote, logFile, log)
	case "soft":
		return doSoftReset(repo, branch, remote, logFile, log)
	case "rebase":
		return doRebase(repo, branch, remote, logFile, log)
	}
	return ResetResult{RelPath: repo.RelPath, Success: false, Error: "unknown mode"}
}

func doHardReset(repo RepoInfo, branch, remote string, logFile *os.File, log func(string, ...any)) ResetResult {
	if inProgress, opName := checkMidOperation(repo.Path); inProgress {
		log("Skipping: mid-%s operation in progress", opName)
		return ResetResult{RelPath: repo.RelPath, Skipped: true, SkipReason: fmt.Sprintf("mid-%s in progress", opName)}
	}

	log("Executing: git reset --hard %s/%s", remote, branch)
	cmd := exec.Command("git", "reset", "--hard", remote+"/"+branch)
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

func doSoftReset(repo RepoInfo, branch, remote string, logFile *os.File, log func(string, ...any)) ResetResult {
	warning := ""
	stagedCheck := exec.Command("git", "diff", "--cached", "--quiet")
	stagedCheck.Dir = repo.Path
	if stagedCheck.Run() != nil {
		warning = "had staged changes before reset"
		log("Warning: staged changes exist; soft reset will merge staged state")
	}

	log("Executing: git reset --soft %s/%s", remote, branch)
	cmd := exec.Command("git", "reset", "--soft", remote+"/"+branch)
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

func doRebase(repo RepoInfo, branch, remote string, logFile *os.File, log func(string, ...any)) ResetResult {
	if checkRebaseInProgress(repo.Path) {
		log("Skipping: rebase already in progress")
		return ResetResult{RelPath: repo.RelPath, Skipped: true, SkipReason: "rebase already in progress"}
	}

	if status := getDirtyStatus(repo.Path); status != "" {
		log("Failed: working tree must be clean for rebase (%s)", status)
		return ResetResult{RelPath: repo.RelPath, Success: false, Error: "working tree must be clean"}
	}

	log("Executing: git rebase %s/%s", remote, branch)
	cmd := exec.Command("git", "rebase", remote+"/"+branch)
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

func resolveRemoteAndBranch(dir, branchArg, defaultRemote string) (remote, branch string) {
	idx := strings.Index(branchArg, "/")
	if idx <= 0 {
		return defaultRemote, branchArg
	}
	candidate := branchArg[:idx]
	rest := branchArg[idx+1:]
	if rest == "" {
		return defaultRemote, branchArg
	}
	if checkRemoteExists(dir, candidate) {
		return candidate, rest
	}
	return defaultRemote, branchArg
}

func checkRemoteExists(dir, remote string) bool {
	cmd := exec.Command("git", "remote", "get-url", remote)
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

func checkBranchOnRemote(dir, branch, remote string) (bool, error) {
	cmd := exec.Command("git", "ls-remote", "--exit-code", "--heads", remote, branch)
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

func checkAlreadyAtTarget(dir, branch, remote string) bool {
	headCmd := exec.Command("git", "rev-parse", "HEAD")
	headCmd.Dir = dir
	headOut, err := headCmd.Output()
	if err != nil {
		return false
	}

	remoteCmd := exec.Command("git", "rev-parse", remote+"/"+branch)
	remoteCmd.Dir = dir
	remoteOut, err := remoteCmd.Output()
	if err != nil {
		return false
	}

	return strings.TrimSpace(string(headOut)) == strings.TrimSpace(string(remoteOut))
}

func fetchBranchFromRemote(dir, branch, remote string, logFile *os.File) error {
	checkCmd := exec.Command("git", "rev-parse", "--is-shallow-repository")
	checkCmd.Dir = dir
	shallowOut, shallowErr := checkCmd.Output()
	isShallow := shallowErr == nil && strings.TrimSpace(string(shallowOut)) == "true"

	args := []string{"fetch", remote}
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
