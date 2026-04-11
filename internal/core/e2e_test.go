package core

// End-to-end tests for the two changes made in this package:
//
//  1. Bug fix: rebase/reset was silently skipped when starting on a
//     different branch (e.g. on "feature", calling -rh main or -rb main
//     would only switch to main but never actually reset/rebase).
//
//  2. Feature: configurable remote name via -r / --remote flag.
//
//  3. Worktree path convention: sibling directory <repo>-<branch-suffix>.
//
//  4. Worktree branch creation fallback to remote-tracking ref when no local branch exists.
//
//  5. Hard reset and rebase always execute even when HEAD already equals origin/branch.
//
// All tests use only local bare repos so no network access is required.

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// makeRepoOnFeatureBranchWithStaleOriginMain builds the exact scenario that
// triggered the bug:
//
//   - A bare remote with two commits on main.
//   - A working repo whose local main is still at the FIRST commit (stale
//     origin/main), while the remote has advanced to the second commit.
//   - The working repo is currently on "feature", not on main.
//
// When processSingleReset is called with target "main", the old code would:
//  1. Switch to main (local main == stale origin/main → checkAlreadyAtTarget true → SKIP).
//
// With the fix, a fetch runs after the switch, origin/main is updated to
// the second commit, and the reset/rebase actually executes.
func makeRepoOnFeatureBranchWithStaleOriginMain(t *testing.T) (repoDir, remoteDir string) {
	t.Helper()
	remoteDir = t.TempDir()
	repoDir = t.TempDir()
	otherDir := t.TempDir()

	runCmd(t, remoteDir, "git", "init", "--bare", "-b", "main")

	// Initial commit: repoDir and origin are in sync.
	createGitRepo(t, repoDir)
	runCmd(t, repoDir, "git", "remote", "add", "origin", remoteDir)
	runCmd(t, repoDir, "git", "push", "origin", "main")
	runCmd(t, repoDir, "git", "fetch", "origin") // local origin/main ref = C0

	// A second clone pushes C1 to origin/main. repoDir does NOT fetch,
	// so its local origin/main tracking ref stays at C0.
	runCmd(t, otherDir, "git", "init", "-b", "main")
	runCmd(t, otherDir, "git", "config", "user.name", "test")
	runCmd(t, otherDir, "git", "config", "user.email", "test@test.com")
	runCmd(t, otherDir, "git", "remote", "add", "origin", remoteDir)
	runCmd(t, otherDir, "git", "fetch", "origin")
	runCmd(t, otherDir, "git", "checkout", "-b", "main", "--track", "origin/main")
	writeFile(t, otherDir, "remote-only.txt", "pushed by other clone")
	runCmd(t, otherDir, "git", "add", ".")
	runCmd(t, otherDir, "git", "commit", "-m", "C1: remote-only commit")
	runCmd(t, otherDir, "git", "push", "origin", "main")

	// repoDir switches to "feature" — it is NOT on main.
	runCmd(t, repoDir, "git", "checkout", "-b", "feature")

	return
}

// --- Bug regression: rebase/reset from a different branch ---

// TestE2EHardResetFromDifferentBranchNotSkipped is the primary regression test.
// Running -rh main while on "feature" must reset the current branch (feature)
// to origin/main without switching branches.
func TestE2EHardResetFromDifferentBranchNotSkipped(t *testing.T) {
	repoDir, _ := makeRepoOnFeatureBranchWithStaleOriginMain(t)

	repo := RepoInfo{Path: repoDir, RelPath: "repo"}
	res := processSingleReset(repo, "main", "hard", "origin", nil)

	if res.Skipped {
		t.Fatalf("expected operation to run, but it was skipped: %q", res.SkipReason)
	}
	if !res.Success {
		t.Fatalf("expected Success=true, got error: %q", res.Error)
	}

	// Current branch must remain "feature" — we never switch.
	branch, err := getCurrentBranch(repoDir)
	if err != nil {
		t.Fatal(err)
	}
	if branch != "feature" {
		t.Errorf("expected current branch to remain 'feature', got %q", branch)
	}

	// The hard reset brought origin/main content onto feature.
	if _, statErr := os.Stat(filepath.Join(repoDir, "remote-only.txt")); os.IsNotExist(statErr) {
		t.Error("expected remote-only.txt to exist after hard reset to origin/main")
	}
}

// TestE2ERebaseFromDifferentBranchNotSkipped verifies -rb main while on "feature":
// rebases feature onto origin/main without switching branches.
func TestE2ERebaseFromDifferentBranchNotSkipped(t *testing.T) {
	repoDir, _ := makeRepoOnFeatureBranchWithStaleOriginMain(t)

	repo := RepoInfo{Path: repoDir, RelPath: "repo"}
	res := processSingleReset(repo, "main", "rebase", "origin", nil)

	if res.Skipped {
		t.Fatalf("expected rebase to run, but it was skipped: %q", res.SkipReason)
	}
	if !res.Success {
		t.Fatalf("expected Success=true, got error: %q", res.Error)
	}

	// Current branch must remain "feature".
	branch, err := getCurrentBranch(repoDir)
	if err != nil {
		t.Fatal(err)
	}
	if branch != "feature" {
		t.Errorf("expected current branch to remain 'feature', got %q", branch)
	}

	if !checkAlreadyAtTarget(repoDir, "main", "origin") {
		t.Error("expected HEAD to be at origin/main after rebase")
	}
}

// TestE2ESoftResetFromDifferentBranchNotSkipped covers the -rs variant:
// soft-resets the current branch (feature) to origin/main without switching.
func TestE2ESoftResetFromDifferentBranchNotSkipped(t *testing.T) {
	repoDir, _ := makeRepoOnFeatureBranchWithStaleOriginMain(t)

	repo := RepoInfo{Path: repoDir, RelPath: "repo"}
	res := processSingleReset(repo, "main", "soft", "origin", nil)

	if res.Skipped {
		t.Fatalf("expected soft reset to run, but it was skipped: %q", res.SkipReason)
	}
	if !res.Success {
		t.Fatalf("expected Success=true, got error: %q", res.Error)
	}

	// Current branch must remain "feature".
	branch, err := getCurrentBranch(repoDir)
	if err != nil {
		t.Fatal(err)
	}
	if branch != "feature" {
		t.Errorf("expected current branch to remain 'feature', got %q", branch)
	}

	if !checkAlreadyAtTarget(repoDir, "main", "origin") {
		t.Error("expected HEAD to be at origin/main after soft reset")
	}
}

// --- Feature: configurable remote name ---

// makeRepoWithCustomRemote creates a repo with the remote named "upstream"
// instead of "origin". Used to verify that passing remote="upstream" works.
func makeRepoWithCustomRemote(t *testing.T, remoteName string) (repoDir, remoteDir string) {
	t.Helper()
	remoteDir = t.TempDir()
	repoDir = t.TempDir()

	runCmd(t, remoteDir, "git", "init", "--bare", "-b", "main")
	createGitRepo(t, repoDir)
	runCmd(t, repoDir, "git", "remote", "add", remoteName, remoteDir)
	runCmd(t, repoDir, "git", "push", remoteName, "main")
	runCmd(t, repoDir, "git", "fetch", remoteName)

	// Add a local commit so the remote tracking ref is behind → reset will fire.
	writeFile(t, repoDir, "local-only.txt", "local commit ahead of remote")
	runCmd(t, repoDir, "git", "add", ".")
	runCmd(t, repoDir, "git", "commit", "-m", "local commit ahead of upstream/main")
	return
}

// TestE2ECustomRemoteProcessSingleReset verifies that processSingleReset uses
// the supplied remote name instead of hardcoded "origin".
func TestE2ECustomRemoteProcessSingleReset(t *testing.T) {
	repoDir, _ := makeRepoWithCustomRemote(t, "upstream")

	// "origin" does not exist — using "origin" must skip.
	repo := RepoInfo{Path: repoDir, RelPath: "repo"}
	resOrigin := processSingleReset(repo, "main", "soft", "origin", nil)
	if !resOrigin.Skipped {
		t.Errorf("expected skip when using non-existent remote 'origin', got success=%v error=%q", resOrigin.Success, resOrigin.Error)
	}
	if !strings.Contains(resOrigin.SkipReason, "origin") {
		t.Errorf("expected skip reason to mention 'origin', got %q", resOrigin.SkipReason)
	}

	// "upstream" exists — reset must succeed.
	resUpstream := processSingleReset(repo, "main", "soft", "upstream", nil)
	if resUpstream.Skipped {
		t.Fatalf("expected reset to run with remote 'upstream', got skipped: %q", resUpstream.SkipReason)
	}
	if !resUpstream.Success {
		t.Fatalf("expected Success=true with remote 'upstream', got error: %q", resUpstream.Error)
	}
}

// TestE2ECustomRemoteSyncBranchViaCfg verifies that syncBranch (soft mode, no
// TTY check) picks up the remote from cfg.Remote.
func TestE2ECustomRemoteSyncBranchViaCfg(t *testing.T) {
	tmpDir := t.TempDir()
	remoteDir := t.TempDir()
	repoDir := filepath.Join(tmpDir, "repo1")
	if err := os.MkdirAll(repoDir, 0755); err != nil {
		t.Fatal(err)
	}

	runCmd(t, remoteDir, "git", "init", "--bare", "-b", "main")
	createGitRepo(t, repoDir)
	runCmd(t, repoDir, "git", "remote", "add", "upstream", remoteDir)
	runCmd(t, repoDir, "git", "push", "upstream", "main")
	runCmd(t, repoDir, "git", "fetch", "upstream")

	// Local commit ahead of upstream/main so the soft reset will actually run.
	writeFile(t, repoDir, "ahead.txt", "ahead of upstream")
	runCmd(t, repoDir, "git", "add", ".")
	runCmd(t, repoDir, "git", "commit", "-m", "local commit ahead of upstream")

	oldStdout := os.Stdout
	pr, pw, _ := os.Pipe()
	os.Stdout = pw

	cfg := mustConfig(t, defaultExcludeDirs, nil, nil, nil, 20, false, "upstream")
	syncBranch(tmpDir, "main", "soft", 2, cfg)

	_ = pw.Close()
	os.Stdout = oldStdout

	outBytes, _ := io.ReadAll(pr)
	output := string(outBytes)

	if !strings.Contains(output, "1 succeeded") {
		t.Errorf("expected '1 succeeded' using remote 'upstream', got: %s", output)
	}
}

// TestE2ECustomRemoteRunFlag verifies that the -r flag is parsed by Run() and
// forwarded to syncBranch. Uses an empty directory so syncBranch exits early
// with "No repos found" — the test just confirms Run() returns no error.
func TestE2ECustomRemoteRunFlag(t *testing.T) {
	tmpDir := t.TempDir()

	origDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(tmpDir); err != nil {
		t.Skip("cannot change directory:", err)
	}
	defer func() { _ = os.Chdir(origDir) }()

	oldStdout := os.Stdout
	_, w, _ := os.Pipe()
	os.Stdout = w

	runErr := Run([]string{"-rs", "main", "-r", "upstream"})

	_ = w.Close()
	os.Stdout = oldStdout

	if runErr != nil {
		t.Fatalf("Run() with -r upstream returned error: %v", runErr)
	}
}

// --- Inline remote/branch notation ---

// TestE2EInlineRemoteReset verifies that passing "origin/main" as the branch
// arg is equivalent to passing "main" with -r origin.
func TestE2EInlineRemoteReset(t *testing.T) {
	repoDir, remoteDir := makeRepoWithRemote(t)

	// Push a new commit so origin/main is ahead of local.
	otherDir := t.TempDir()
	runCmd(t, otherDir, "git", "init", "-b", "main")
	runCmd(t, otherDir, "git", "config", "user.name", "test")
	runCmd(t, otherDir, "git", "config", "user.email", "test@test.com")
	runCmd(t, otherDir, "git", "remote", "add", "origin", remoteDir)
	runCmd(t, otherDir, "git", "fetch", "origin")
	runCmd(t, otherDir, "git", "checkout", "-b", "main", "--track", "origin/main")
	writeFile(t, otherDir, "inline-remote.txt", "content")
	runCmd(t, otherDir, "git", "add", ".")
	runCmd(t, otherDir, "git", "commit", "-m", "new commit")
	runCmd(t, otherDir, "git", "push", "origin", "main")

	repo := RepoInfo{Path: repoDir, RelPath: "repo"}

	// Pass "origin/main" as the branch arg — remote is extracted automatically.
	res := processSingleReset(repo, "origin/main", "hard", "origin", nil)
	if !res.Success {
		t.Fatalf("expected Success=true, got error=%q skipped=%v reason=%q", res.Error, res.Skipped, res.SkipReason)
	}
	if _, statErr := os.Stat(filepath.Join(repoDir, "inline-remote.txt")); os.IsNotExist(statErr) {
		t.Error("expected inline-remote.txt to exist after reset to origin/main")
	}
}

// TestE2EInlineRemoteResetSlashedBranch verifies a slashed branch name like
// "feat/branch1" is not mistakenly split — it is used as the full branch name
// under the default remote.
func TestE2EInlineRemoteResetSlashedBranch(t *testing.T) {
	repoDir, remoteDir := makeRepoWithRemote(t)

	// Create and push a slashed branch on the remote.
	otherDir := t.TempDir()
	runCmd(t, otherDir, "git", "init", "-b", "main")
	runCmd(t, otherDir, "git", "config", "user.name", "test")
	runCmd(t, otherDir, "git", "config", "user.email", "test@test.com")
	runCmd(t, otherDir, "git", "remote", "add", "origin", remoteDir)
	runCmd(t, otherDir, "git", "fetch", "origin")
	runCmd(t, otherDir, "git", "checkout", "-b", "main", "--track", "origin/main")
	runCmd(t, otherDir, "git", "checkout", "-b", "feat/branch1")
	writeFile(t, otherDir, "feat-file.txt", "feature content")
	runCmd(t, otherDir, "git", "add", ".")
	runCmd(t, otherDir, "git", "commit", "-m", "feat commit")
	runCmd(t, otherDir, "git", "push", "origin", "feat/branch1")

	// "feat" is not a remote — the full arg is used as the branch name.
	repo := RepoInfo{Path: repoDir, RelPath: "repo"}
	res := processSingleReset(repo, "feat/branch1", "hard", "origin", nil)
	if !res.Success {
		t.Fatalf("expected Success=true resetting to origin/feat/branch1, got error=%q skipped=%v reason=%q",
			res.Error, res.Skipped, res.SkipReason)
	}
	if _, statErr := os.Stat(filepath.Join(repoDir, "feat-file.txt")); os.IsNotExist(statErr) {
		t.Error("expected feat-file.txt to exist after reset to origin/feat/branch1")
	}
}

// TestE2ECustomRemoteSwitchBranch verifies that switchBranches uses cfg.Remote
// (set via -r) when fetching a branch that doesn't exist locally.
func TestE2ECustomRemoteSwitchBranch(t *testing.T) {
	remoteDir := t.TempDir()
	repoDir := t.TempDir()

	runCmd(t, remoteDir, "git", "init", "--bare", "-b", "main")
	createGitRepo(t, repoDir)
	runCmd(t, repoDir, "git", "remote", "add", "upstream", remoteDir)
	runCmd(t, repoDir, "git", "push", "upstream", "main")
	runCmd(t, repoDir, "git", "fetch", "upstream")

	otherDir := t.TempDir()
	runCmd(t, otherDir, "git", "init", "-b", "main")
	runCmd(t, otherDir, "git", "config", "user.name", "test")
	runCmd(t, otherDir, "git", "config", "user.email", "test@test.com")
	runCmd(t, otherDir, "git", "remote", "add", "upstream", remoteDir)
	runCmd(t, otherDir, "git", "fetch", "upstream")
	runCmd(t, otherDir, "git", "checkout", "-b", "main", "--track", "upstream/main")
	runCmd(t, otherDir, "git", "checkout", "-b", "develop")
	writeFile(t, otherDir, "develop.txt", "develop content")
	runCmd(t, otherDir, "git", "add", ".")
	runCmd(t, otherDir, "git", "commit", "-m", "develop branch commit")
	runCmd(t, otherDir, "git", "push", "upstream", "develop")

	repo := RepoInfo{Path: repoDir, RelPath: "repo"}
	res := processSingleRepo(repo, "develop", "upstream", nil)
	if !res.Success {
		t.Fatalf("expected switch to develop via upstream to succeed, got error: %q", res.Error)
	}

	branch, err := getCurrentBranch(repoDir)
	if err != nil {
		t.Fatal(err)
	}
	if branch != "develop" {
		t.Errorf("expected current branch 'develop', got %q", branch)
	}
}

// --- Worktree path convention ---

// TestWorktreePathSibling verifies the sibling directory naming convention:
// <parent>/<repo-basename>-<branch-suffix>.
func TestWorktreePathSibling(t *testing.T) {
	cases := []struct {
		repoPath string
		branch   string
		want     string
	}{
		{"/projects/projA", "feat/abc", "/projects/projA-abc"},
		{"/projects/projA", "test", "/projects/projA-test"},
		{"/projects/projA", "fix/login/v2", "/projects/projA-v2"},
		{"/projects/my-service", "release/1.0", "/projects/my-service-1.0"},
	}
	for _, tc := range cases {
		got := worktreePath(filepath.FromSlash(tc.repoPath), tc.branch)
		want := filepath.FromSlash(tc.want)
		if got != want {
			t.Errorf("worktreePath(%q, %q) = %q, want %q", tc.repoPath, tc.branch, got, want)
		}
	}
}

// --- Worktree creation with remote-only base ---

// TestWorktreeCreateRemoteOnlyBase verifies that when the base branch (e.g.
// "main") only exists as a remote-tracking ref and not as a local branch,
// worktreeCreate still creates the worktree successfully.
func TestWorktreeCreateRemoteOnlyBase(t *testing.T) {
	remoteDir := t.TempDir()
	parentDir := t.TempDir()
	repoDir := filepath.Join(parentDir, "projA")

	// Set up: bare remote + working repo whose only branch is main (no local master).
	runCmd(t, remoteDir, "git", "init", "--bare", "-b", "main")
	createGitRepo(t, repoDir)
	runCmd(t, repoDir, "git", "remote", "add", "origin", remoteDir)
	runCmd(t, repoDir, "git", "push", "origin", "main")
	runCmd(t, repoDir, "git", "fetch", "origin")

	// No local "main" branch yet after fetch — only origin/main tracking ref exists.
	// Delete the local main branch to simulate the "remote-only base" scenario.
	runCmd(t, repoDir, "git", "checkout", "--detach")
	runCmd(t, repoDir, "git", "branch", "-D", "main")

	cfg := mustConfig(t, defaultExcludeDirs, nil, nil, nil, 20, false, "origin")
	worktreeCreate(parentDir, "feat/my-task", "main", 2, cfg)

	// Worktree must exist at the sibling path projA-my-task.
	wtPath := filepath.Join(parentDir, "projA-my-task")
	if _, err := os.Stat(wtPath); err != nil {
		t.Fatalf("expected worktree at %s, got: %v", wtPath, err)
	}
}

// --- Hard reset and rebase run even when already at target ---

// TestHardResetRunsWhenAlreadyAtTarget verifies that -rh does NOT skip when
// HEAD already equals origin/main (working tree may be dirty with tracked changes).
func TestHardResetRunsWhenAlreadyAtTarget(t *testing.T) {
	repoDir, _ := makeRepoWithRemote(t)

	// Modify a tracked file — HEAD is still at origin/main but working tree is dirty.
	writeFile(t, repoDir, "README.md", "modified content — should be discarded")

	repo := RepoInfo{Path: repoDir, RelPath: "repo"}
	res := processSingleReset(repo, "main", "hard", "origin", nil)

	if res.Skipped {
		t.Fatalf("hard reset must not be skipped even when HEAD == origin/main; got skipped: %q", res.SkipReason)
	}
	if !res.Success {
		t.Fatalf("expected Success=true, got error: %q", res.Error)
	}

	// README.md must be restored to its committed content.
	content, err := os.ReadFile(filepath.Join(repoDir, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(content), "modified content") {
		t.Error("expected hard reset to restore README.md to committed content")
	}
}

// TestRebaseRunsWhenAlreadyAtTarget verifies that -rb does NOT skip when
// HEAD already equals origin/main; git handles the no-op gracefully.
func TestRebaseRunsWhenAlreadyAtTarget(t *testing.T) {
	repoDir, _ := makeRepoWithRemote(t)

	repo := RepoInfo{Path: repoDir, RelPath: "repo"}
	res := processSingleReset(repo, "main", "rebase", "origin", nil)

	if res.Skipped {
		t.Fatalf("rebase must not be skipped even when HEAD == origin/main; got skipped: %q", res.SkipReason)
	}
	if !res.Success {
		t.Fatalf("expected Success=true, got error: %q", res.Error)
	}
}
