package core

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
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

func syncBranch(ctx context.Context, root, branch, posBranch, mode string, workers int, cfg *Config) error {
	repos, total := discoverRepos(root, workers, cfg, false)
	if repos == nil {
		return nil
	}

	plan := classifyReset(branch, posBranch, cfg.Remote, cfg.RemoteExplicit)
	displayRef := plan.displayRef()

	if mode == "hard" || mode == "rebase" {
		fileInfo, statErr := os.Stdin.Stat()
		if statErr != nil || (fileInfo.Mode()&os.ModeCharDevice) == 0 {
			return fmt.Errorf("stdin is not a terminal; destructive operations require interactive confirmation — use -rs for non-interactive use")
		}

		dirtyRepos := preflightScan(ctx, repos, workers, plan, mode)
		if !PromptConfirmDestructive(operationDescription(mode, displayRef), len(repos), dirtyRepos) {
			fmt.Println("Aborted.")
			return nil
		}
	}

	opDesc := operationDescription(mode, displayRef)
	fmt.Println(StyleInfo.Render(fmt.Sprintf("Found %d repos (filtered from %d discovered), running '%s' with %d workers...",
		len(repos), total, opDesc, min(workers, len(repos)))))

	logManager, err := NewLogManager()
	if err != nil {
		return fmt.Errorf("log manager: %w", err)
	}

	progress := NewProgressState(repos, opDesc, cfg.PageSize)
	progress.StartInput()

	results := runPool(ctx, repos, workers, func(ctx context.Context, r RepoInfo) ResetResult {
		progress.UpdateStatus(r.RelPath, statusProcessing, "")

		logFile, _ := logManager.CreateLogFile(r.RelPath)
		target := finalizeResetTarget(plan, r.Path)
		res := processSingleReset(ctx, r, target, mode, logFile)
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

	var sum strings.Builder
	sum.WriteString(StyleBold.Render("--- Summary ---") + "\n")
	fmt.Fprintf(&sum, "Ran '%s' across %d repos:\n", opDesc, len(repos))
	fmt.Fprintf(&sum, "  %s succeeded\n", StyleSuccess.Render(fmt.Sprintf("%d", succeeded)))
	fmt.Fprintf(&sum, "  %s failed", StyleFailed.Render(fmt.Sprintf("%d", failed)))
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
		fmt.Fprintf(&sum, "\n  %s skipped (%s)", StyleSkipped.Render(fmt.Sprintf("%d", skipped)), strings.Join(parts, ", "))
	}

	if progress.FinishAndPromptViewLogs(sum.String()) {
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

func operationDescription(mode, ref string) string {
	switch mode {
	case "soft":
		return fmt.Sprintf("git reset --soft %s", ref)
	case "hard":
		return fmt.Sprintf("git reset --hard %s", ref)
	case "rebase":
		return fmt.Sprintf("git rebase %s", ref)
	}
	return "unknown operation"
}

func preflightScan(ctx context.Context, repos []RepoInfo, workers int, plan resetPlan, mode string) []repoPreflightInfo {
	results := runPool(ctx, repos, workers, func(ctx context.Context, r RepoInfo) repoPreflightInfo {
		status, err := getDirtyStatus(ctx, r.Path)
		if err != nil {
			status = "status check failed"
		}
		if mode == "hard" {
			target := finalizeResetTarget(plan, r.Path)
			if n := countUnpushedCommits(ctx, r.Path, target.ref()); n > 0 {
				unpushed := fmt.Sprintf("%d unpushed commit(s)", n)
				if status == "" {
					status = unpushed
				} else {
					status += ", " + unpushed
				}
			}
		}
		return repoPreflightInfo{RelPath: r.RelPath, Path: r.Path, DirtyStatus: status}
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

// countUnpushedCommits counts commits on HEAD that the reset target lacks, i.e.
// what a hard reset would discard beyond working-tree changes. Best-effort: an
// unresolvable target (missing branch, no remote-tracking ref yet) counts as 0;
// the actual reset handles those cases itself.
func countUnpushedCommits(ctx context.Context, dir, ref string) int {
	cmd := exec.CommandContext(ctx, "git", "rev-list", "--count", ref+"..HEAD")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return 0
	}
	n, _ := strconv.Atoi(strings.TrimSpace(string(out)))
	return n
}

func getDirtyStatus(ctx context.Context, dir string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "-c", "core.fsmonitor=false", "status", "--porcelain")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	trimmed := strings.TrimSpace(string(out))
	if trimmed == "" {
		return "", nil
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
		return "staged + unstaged changes", nil
	case hasStaged:
		return "staged changes", nil
	case hasUnstaged:
		return "unstaged changes", nil
	}
	return "changes", nil
}

// resetTarget is the resolved destination of a reset/rebase in a single repo.
type resetTarget struct {
	isRemote       bool
	remote         string // valid only when isRemote
	branch         string
	fallbackRemote string // local mode only: remote to try if the local branch is missing
	verified       bool   // remote existence + branch presence already confirmed (skip re-probe)
}

func (t resetTarget) ref() string {
	if t.isRemote {
		return t.remote + "/" + t.branch
	}
	return t.branch
}

// resetPlan is the remote/branch decision derivable from the CLI args alone,
// before any repo is inspected. It is the single source of truth for both the
// human-facing display (displayRef) and the per-repo target (finalizeResetTarget).
type resetPlan struct {
	explicitRemote string // two-token or -r remote; "" => auto (inline-or-local, per repo)
	branch         string // branch component when explicitRemote != ""
	arg            string // raw arg, used in auto mode
	fallbackRemote string // default remote for the local fallback in auto mode
}

// classifyReset interprets a reset/rebase argument the way `git reset` does:
//   - explicit two-token form (`origin main`) → remote `origin`, branch `main`
//   - explicit -r flag → that remote, branch is the whole arg
//   - otherwise auto: inline `remote/branch` where the prefix is a real remote,
//     else the local ref named by the whole arg (with defaultRemote as fallback).
func classifyReset(arg, posBranch, flagRemote string, flagRemoteSet bool) resetPlan {
	if posBranch != "" {
		return resetPlan{explicitRemote: arg, branch: posBranch}
	}
	if flagRemoteSet {
		return resetPlan{explicitRemote: flagRemote, branch: arg}
	}
	return resetPlan{arg: arg, fallbackRemote: flagRemote}
}

// displayRef renders the human-facing target derived purely from the args. For
// the auto-local case it spells out the remote fallback so the destructive
// confirmation prompt and summary never understate what may run.
func (p resetPlan) displayRef() string {
	if p.explicitRemote != "" {
		return p.explicitRemote + "/" + p.branch
	}
	if strings.Contains(p.arg, "/") {
		return p.arg
	}
	if p.fallbackRemote != "" {
		return fmt.Sprintf("%s (or %s/%s if not a local branch)", p.arg, p.fallbackRemote, p.arg)
	}
	return p.arg
}

// finalizeResetTarget resolves a plan against a single repo's remotes.
func finalizeResetTarget(plan resetPlan, dir string) resetTarget {
	if plan.explicitRemote != "" {
		return resetTarget{isRemote: true, remote: plan.explicitRemote, branch: plan.branch}
	}
	if remote, branch, ok := splitRemoteBranch(dir, plan.arg); ok {
		return resetTarget{isRemote: true, remote: remote, branch: branch}
	}
	return resetTarget{isRemote: false, branch: plan.arg, fallbackRemote: plan.fallbackRemote}
}

// resolveResetTarget is the per-repo entry point: classify the args, then resolve
// against this repo.
func resolveResetTarget(dir, arg, posBranch, flagRemote string, flagRemoteSet bool) resetTarget {
	return finalizeResetTarget(classifyReset(arg, posBranch, flagRemote, flagRemoteSet), dir)
}

// splitRemoteBranch splits `remote/branch` only when the prefix is a real remote
// in dir. Shared by reset and divergence so the parsing rule lives in one place.
func splitRemoteBranch(dir, arg string) (remote, branch string, ok bool) {
	if i := strings.Index(arg, "/"); i > 0 && i < len(arg)-1 {
		if candidate := arg[:i]; checkRemoteExists(dir, candidate) {
			return candidate, arg[i+1:], true
		}
	}
	return "", "", false
}

func processSingleReset(ctx context.Context, repo RepoInfo, target resetTarget, mode string, logFile *os.File) ResetResult {
	log := func(format string, args ...any) {
		if logFile != nil {
			_, _ = fmt.Fprintf(logFile, format+"\n", args...)
		}
	}

	log("=== Processing %s ===", repo.RelPath)

	if !checkHasCommits(repo.Path) {
		log("Skipping: no commits")
		return ResetResult{RelPath: repo.RelPath, Skipped: true, SkipReason: "no commits"}
	}

	if checkDetachedHEAD(repo.Path) {
		log("Skipping: detached HEAD")
		return ResetResult{RelPath: repo.RelPath, Skipped: true, SkipReason: "detached HEAD"}
	}

	if !target.isRemote {
		effective, res, done := resolveLocalTarget(ctx, repo, target, log)
		if done {
			return res
		}
		target = effective
	}

	log("Target: %s, Mode: %s", target.ref(), mode)

	if target.isRemote {
		if !target.verified {
			if !checkRemoteExists(repo.Path, target.remote) {
				log("Skipping: no %s remote", target.remote)
				return ResetResult{RelPath: repo.RelPath, Skipped: true, SkipReason: "no " + target.remote + " remote"}
			}

			found, netErr := checkBranchOnRemote(ctx, repo.Path, target.branch, target.remote)
			if netErr != nil {
				log("Error checking remote branch: %v", netErr)
				return ResetResult{RelPath: repo.RelPath, Success: false, Error: fmt.Sprintf("network error: %v", netErr)}
			}
			if !found {
				log("Skipping: branch not on %s", target.remote)
				return ResetResult{RelPath: repo.RelPath, Skipped: true, SkipReason: "branch not on " + target.remote}
			}
		}

		log("Fetching to update %s ref", target.ref())
		if fetchErr := fetchBranchFromRemote(ctx, repo.Path, target.branch, target.remote, logFile); fetchErr != nil {
			log("Fetch failed: %v", fetchErr)
			return ResetResult{RelPath: repo.RelPath, Success: false, Error: "fetch failed"}
		}
	}

	if mode == "soft" && checkAlreadyAtTarget(repo.Path, target.ref()) {
		log("Skipping: already up to date")
		return ResetResult{RelPath: repo.RelPath, Skipped: true, SkipReason: "already up to date"}
	}

	switch mode {
	case "hard":
		return doHardReset(ctx, repo, target, logFile, log)
	case "soft":
		return doSoftReset(ctx, repo, target, logFile, log)
	case "rebase":
		return doRebase(ctx, repo, target, logFile, log)
	}
	return ResetResult{RelPath: repo.RelPath, Success: false, Error: "unknown mode"}
}

// resolveLocalTarget settles a local-mode target against a single repo. If the
// local branch exists it stays local; otherwise it falls back to the default
// remote when the branch is present there. Returns done=true with a skip result
// when the branch exists neither locally nor on the remote.
func resolveLocalTarget(ctx context.Context, repo RepoInfo, target resetTarget, log func(string, ...any)) (resetTarget, ResetResult, bool) {
	if checkLocalBranchExists(repo.Path, target.branch) {
		return target, ResetResult{}, false
	}

	remote := target.fallbackRemote
	if remote != "" && checkRemoteExists(repo.Path, remote) {
		found, netErr := checkBranchOnRemote(ctx, repo.Path, target.branch, remote)
		if netErr != nil {
			log("Error checking remote branch: %v", netErr)
			return target, ResetResult{RelPath: repo.RelPath, Success: false, Error: fmt.Sprintf("network error: %v", netErr)}, true
		}
		if found {
			log("Local branch %s missing; falling back to %s/%s", target.branch, remote, target.branch)
			return resetTarget{isRemote: true, remote: remote, branch: target.branch, verified: true}, ResetResult{}, false
		}
	}

	log("Skipping: branch %s not found locally or on %s", target.branch, remote)
	return target, ResetResult{RelPath: repo.RelPath, Skipped: true, SkipReason: "branch " + target.branch + " not found locally or on " + remote}, true
}

func checkLocalBranchExists(dir, branch string) bool {
	cmd := exec.Command("git", "show-ref", "--verify", "--quiet", "refs/heads/"+branch)
	cmd.Dir = dir
	return cmd.Run() == nil
}

func doHardReset(ctx context.Context, repo RepoInfo, target resetTarget, logFile *os.File, log func(string, ...any)) ResetResult {
	if inProgress, opName := checkMidOperation(repo.Path); inProgress {
		log("Skipping: mid-%s operation in progress", opName)
		return ResetResult{RelPath: repo.RelPath, Skipped: true, SkipReason: fmt.Sprintf("mid-%s in progress", opName)}
	}

	log("Executing: git reset --hard %s", target.ref())
	cmd := exec.CommandContext(ctx, "git", "reset", "--hard", target.ref())
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

func doSoftReset(ctx context.Context, repo RepoInfo, target resetTarget, logFile *os.File, log func(string, ...any)) ResetResult {
	warning := ""
	stagedCheck := exec.Command("git", "diff", "--cached", "--quiet")
	stagedCheck.Dir = repo.Path
	if stagedCheck.Run() != nil {
		warning = "had staged changes before reset"
		log("Warning: staged changes exist; soft reset will merge staged state")
	}

	log("Executing: git reset --soft %s", target.ref())
	cmd := exec.CommandContext(ctx, "git", "reset", "--soft", target.ref())
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

func doRebase(ctx context.Context, repo RepoInfo, target resetTarget, logFile *os.File, log func(string, ...any)) ResetResult {
	if checkRebaseInProgress(repo.Path) {
		log("Skipping: rebase already in progress")
		return ResetResult{RelPath: repo.RelPath, Skipped: true, SkipReason: "rebase already in progress"}
	}

	status, statusErr := getDirtyStatus(ctx, repo.Path)
	if statusErr != nil {
		log("Failed: could not check working tree status: %v", statusErr)
		return ResetResult{RelPath: repo.RelPath, Success: false, Error: "status check failed"}
	}
	if status != "" {
		log("Failed: working tree must be clean for rebase (%s)", status)
		return ResetResult{RelPath: repo.RelPath, Success: false, Error: "working tree must be clean"}
	}

	log("Executing: git rebase %s", target.ref())
	cmd := exec.CommandContext(ctx, "git", "rebase", target.ref())
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

func checkBranchOnRemote(ctx context.Context, dir, branch, remote string) (bool, error) {
	cmd := exec.CommandContext(ctx, "git", "-c", "protocol.ext.allow=never", "ls-remote", "--exit-code", "--heads", remote, branch)
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

func checkAlreadyAtTarget(dir, ref string) bool {
	headCmd := exec.Command("git", "rev-parse", "HEAD")
	headCmd.Dir = dir
	headOut, err := headCmd.Output()
	if err != nil {
		return false
	}

	targetCmd := exec.Command("git", "rev-parse", ref)
	targetCmd.Dir = dir
	targetOut, err := targetCmd.Output()
	if err != nil {
		return false
	}

	return strings.TrimSpace(string(headOut)) == strings.TrimSpace(string(targetOut))
}

func fetchBranchFromRemote(ctx context.Context, dir, branch, remote string, logFile *os.File) error {
	checkCmd := exec.Command("git", "rev-parse", "--is-shallow-repository")
	checkCmd.Dir = dir
	shallowOut, shallowErr := checkCmd.Output()
	isShallow := shallowErr == nil && strings.TrimSpace(string(shallowOut)) == "true"

	args := []string{"-c", "protocol.ext.allow=never", "fetch", remote}
	if isShallow {
		args = append(args, "--depth=1")
	}
	args = append(args, branch)

	fetchCmd := exec.CommandContext(ctx, "git", args...)
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
