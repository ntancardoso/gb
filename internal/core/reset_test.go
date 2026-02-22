package core

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// --- helpers ---

// makeRepoWithRemote creates a local repo wired to a local bare remote.
// The initial commit is pushed so origin/main exists.
func makeRepoWithRemote(t *testing.T) (repoDir, remoteDir string) {
	t.Helper()
	remoteDir = t.TempDir()
	repoDir = t.TempDir()

	runCmd(t, remoteDir, "git", "init", "--bare", "-b", "main")
	createGitRepo(t, repoDir)
	runCmd(t, repoDir, "git", "remote", "add", "origin", remoteDir)
	runCmd(t, repoDir, "git", "push", "origin", "main")
	runCmd(t, repoDir, "git", "fetch", "origin")
	return
}

// makeRepoAheadOfOrigin builds on makeRepoWithRemote and adds one local
// commit that has NOT been pushed, so local is one ahead of origin/main.
func makeRepoAheadOfOrigin(t *testing.T) (repoDir, remoteDir string) {
	t.Helper()
	repoDir, remoteDir = makeRepoWithRemote(t)

	writeFile(t, repoDir, "local.txt", "local content")
	runCmd(t, repoDir, "git", "add", ".")
	runCmd(t, repoDir, "git", "commit", "-m", "local commit ahead of origin")
	return
}

// makeRepoWithConflict creates a repo and a remote where local main and
// origin/main have diverged on "conflict.txt", guaranteeing a rebase conflict.
func makeRepoWithConflict(t *testing.T) (repoDir, remoteDir string) {
	t.Helper()
	remoteDir = t.TempDir()
	repoDir = t.TempDir()
	otherDir := t.TempDir()

	// Base: bare remote + working repo with an initial commit that includes conflict.txt.
	runCmd(t, remoteDir, "git", "init", "--bare", "-b", "main")
	createGitRepo(t, repoDir)
	writeFile(t, repoDir, "conflict.txt", "base\n")
	runCmd(t, repoDir, "git", "add", "conflict.txt")
	runCmd(t, repoDir, "git", "commit", "-m", "add conflict.txt")
	runCmd(t, repoDir, "git", "remote", "add", "origin", remoteDir)
	runCmd(t, repoDir, "git", "push", "origin", "main")

	// Another clone pushes a conflicting change to origin.
	runCmd(t, otherDir, "git", "init", "-b", "main")
	runCmd(t, otherDir, "git", "config", "user.name", "test")
	runCmd(t, otherDir, "git", "config", "user.email", "test@test.com")
	runCmd(t, otherDir, "git", "remote", "add", "origin", remoteDir)
	runCmd(t, otherDir, "git", "fetch", "origin")
	runCmd(t, otherDir, "git", "checkout", "-b", "main", "--track", "origin/main")
	writeFile(t, otherDir, "conflict.txt", "origin version\n")
	runCmd(t, otherDir, "git", "add", "conflict.txt")
	runCmd(t, otherDir, "git", "commit", "-m", "origin conflicting change")
	runCmd(t, otherDir, "git", "push", "origin", "main")

	// repoDir fetches (so origin/main ref is updated) then makes its own diverging commit.
	runCmd(t, repoDir, "git", "fetch", "origin")
	writeFile(t, repoDir, "conflict.txt", "local version\n")
	runCmd(t, repoDir, "git", "add", "conflict.txt")
	runCmd(t, repoDir, "git", "commit", "-m", "local conflicting change")

	return
}

// --- helper checks ---

func TestCheckOriginExists(t *testing.T) {
	t.Run("no origin", func(t *testing.T) {
		tmpDir := t.TempDir()
		createGitRepo(t, tmpDir)
		if checkOriginExists(tmpDir) {
			t.Error("expected no origin remote")
		}
	})

	t.Run("with origin", func(t *testing.T) {
		repoDir, _ := makeRepoWithRemote(t)
		if !checkOriginExists(repoDir) {
			t.Error("expected origin remote to exist")
		}
	})
}

func TestCheckHasCommits(t *testing.T) {
	t.Run("empty repo", func(t *testing.T) {
		tmpDir := t.TempDir()
		runCmd(t, tmpDir, "git", "init", "-b", "main")
		runCmd(t, tmpDir, "git", "config", "user.name", "test")
		runCmd(t, tmpDir, "git", "config", "user.email", "test@test.com")
		if checkHasCommits(tmpDir) {
			t.Error("expected no commits")
		}
	})

	t.Run("repo with commits", func(t *testing.T) {
		tmpDir := t.TempDir()
		createGitRepo(t, tmpDir)
		if !checkHasCommits(tmpDir) {
			t.Error("expected commits to exist")
		}
	})
}

func TestCheckDetachedHEAD(t *testing.T) {
	t.Run("normal branch", func(t *testing.T) {
		tmpDir := t.TempDir()
		createGitRepo(t, tmpDir)
		if checkDetachedHEAD(tmpDir) {
			t.Error("expected HEAD not detached")
		}
	})

	t.Run("detached HEAD", func(t *testing.T) {
		tmpDir := t.TempDir()
		createGitRepo(t, tmpDir)
		hash := strings.TrimSpace(string(runCmdOutput(t, tmpDir, "git", "rev-parse", "HEAD")))
		runCmd(t, tmpDir, "git", "checkout", hash)
		if !checkDetachedHEAD(tmpDir) {
			t.Error("expected detached HEAD")
		}
	})
}

func TestCheckBranchOnOrigin(t *testing.T) {
	repoDir, _ := makeRepoWithRemote(t)

	t.Run("existing branch", func(t *testing.T) {
		found, err := checkBranchOnOrigin(repoDir, "main")
		if err != nil {
			t.Fatal(err)
		}
		if !found {
			t.Error("expected main to be on origin")
		}
	})

	t.Run("non-existent branch", func(t *testing.T) {
		found, err := checkBranchOnOrigin(repoDir, "does-not-exist-xyz")
		if err != nil {
			t.Fatal(err)
		}
		if found {
			t.Error("expected branch to not be on origin")
		}
	})
}

func TestCheckAlreadyAtTarget(t *testing.T) {
	t.Run("already at target", func(t *testing.T) {
		repoDir, _ := makeRepoWithRemote(t)
		if !checkAlreadyAtTarget(repoDir, "main") {
			t.Error("expected already at origin/main")
		}
	})

	t.Run("ahead of target", func(t *testing.T) {
		repoDir, _ := makeRepoAheadOfOrigin(t)
		if checkAlreadyAtTarget(repoDir, "main") {
			t.Error("expected NOT at origin/main (local is ahead)")
		}
	})
}

func TestCheckMidOperation(t *testing.T) {
	t.Run("no operation in progress", func(t *testing.T) {
		tmpDir := t.TempDir()
		createGitRepo(t, tmpDir)
		inProgress, _ := checkMidOperation(tmpDir)
		if inProgress {
			t.Error("expected no mid-operation")
		}
	})

	t.Run("mid-merge", func(t *testing.T) {
		tmpDir := t.TempDir()
		createGitRepo(t, tmpDir)
		mergeHead := filepath.Join(tmpDir, ".git", "MERGE_HEAD")
		if err := os.WriteFile(mergeHead, []byte("abc123\n"), 0644); err != nil {
			t.Fatal(err)
		}
		inProgress, opName := checkMidOperation(tmpDir)
		if !inProgress {
			t.Error("expected mid-merge operation")
		}
		if opName != "merge" {
			t.Errorf("expected 'merge', got %q", opName)
		}
	})

	t.Run("mid-cherry-pick", func(t *testing.T) {
		tmpDir := t.TempDir()
		createGitRepo(t, tmpDir)
		cpHead := filepath.Join(tmpDir, ".git", "CHERRY_PICK_HEAD")
		if err := os.WriteFile(cpHead, []byte("abc123\n"), 0644); err != nil {
			t.Fatal(err)
		}
		inProgress, opName := checkMidOperation(tmpDir)
		if !inProgress {
			t.Error("expected mid-cherry-pick")
		}
		if opName != "cherry-pick" {
			t.Errorf("expected 'cherry-pick', got %q", opName)
		}
	})

	t.Run("mid-revert", func(t *testing.T) {
		tmpDir := t.TempDir()
		createGitRepo(t, tmpDir)
		revertHead := filepath.Join(tmpDir, ".git", "REVERT_HEAD")
		if err := os.WriteFile(revertHead, []byte("abc123\n"), 0644); err != nil {
			t.Fatal(err)
		}
		inProgress, opName := checkMidOperation(tmpDir)
		if !inProgress {
			t.Error("expected mid-revert operation")
		}
		if opName != "revert" {
			t.Errorf("expected 'revert', got %q", opName)
		}
	})
}

func TestCheckRebaseInProgress(t *testing.T) {
	t.Run("no rebase", func(t *testing.T) {
		tmpDir := t.TempDir()
		createGitRepo(t, tmpDir)
		if checkRebaseInProgress(tmpDir) {
			t.Error("expected no rebase in progress")
		}
	})

	t.Run("rebase-merge directory present", func(t *testing.T) {
		tmpDir := t.TempDir()
		createGitRepo(t, tmpDir)
		rebaseMerge := filepath.Join(tmpDir, ".git", "rebase-merge")
		if err := os.MkdirAll(rebaseMerge, 0755); err != nil {
			t.Fatal(err)
		}
		if !checkRebaseInProgress(tmpDir) {
			t.Error("expected rebase in progress")
		}
	})

	t.Run("rebase-apply directory present", func(t *testing.T) {
		tmpDir := t.TempDir()
		createGitRepo(t, tmpDir)
		rebaseApply := filepath.Join(tmpDir, ".git", "rebase-apply")
		if err := os.MkdirAll(rebaseApply, 0755); err != nil {
			t.Fatal(err)
		}
		if !checkRebaseInProgress(tmpDir) {
			t.Error("expected rebase in progress")
		}
	})
}

func TestGetDirtyStatus(t *testing.T) {
	// Each sub-test uses its own repo to avoid shared-state ordering issues
	// and to sidestep CRLF normalization behaviour on Windows (which can make
	// modifications to tracked files appear staged rather than unstaged).

	t.Run("clean", func(t *testing.T) {
		tmpDir := t.TempDir()
		createGitRepo(t, tmpDir)
		if status := getDirtyStatus(tmpDir); status != "" {
			t.Errorf("expected clean, got %q", status)
		}
	})

	t.Run("unstaged changes (untracked file)", func(t *testing.T) {
		tmpDir := t.TempDir()
		createGitRepo(t, tmpDir)
		// An untracked file shows as "??" in porcelain — unambiguous on all platforms.
		writeFile(t, tmpDir, "untracked.txt", "new content")
		if status := getDirtyStatus(tmpDir); status != "unstaged changes" {
			t.Errorf("expected 'unstaged changes', got %q", status)
		}
	})

	t.Run("staged changes", func(t *testing.T) {
		tmpDir := t.TempDir()
		createGitRepo(t, tmpDir)
		writeFile(t, tmpDir, "staged.txt", "staged content")
		runCmd(t, tmpDir, "git", "add", "staged.txt")
		if status := getDirtyStatus(tmpDir); status != "staged changes" {
			t.Errorf("expected 'staged changes', got %q", status)
		}
	})

	t.Run("staged and unstaged", func(t *testing.T) {
		tmpDir := t.TempDir()
		createGitRepo(t, tmpDir)
		writeFile(t, tmpDir, "staged.txt", "staged content")
		runCmd(t, tmpDir, "git", "add", "staged.txt")
		writeFile(t, tmpDir, "untracked.txt", "untracked content")
		if status := getDirtyStatus(tmpDir); status != "staged + unstaged changes" {
			t.Errorf("expected 'staged + unstaged changes', got %q", status)
		}
	})
}

// --- processSingleReset: skip scenarios ---

func TestProcessSingleResetSkipNoOrigin(t *testing.T) {
	tmpDir := t.TempDir()
	createGitRepo(t, tmpDir)

	repo := RepoInfo{Path: tmpDir, RelPath: "test"}
	res := processSingleReset(repo, "main", "soft", nil)

	if !res.Skipped {
		t.Error("expected Skipped=true for no origin")
	}
	if res.SkipReason != "no origin remote" {
		t.Errorf("unexpected skip reason: %q", res.SkipReason)
	}
}

func TestProcessSingleResetSkipNoCommits(t *testing.T) {
	tmpDir := t.TempDir()
	runCmd(t, tmpDir, "git", "init", "-b", "main")
	runCmd(t, tmpDir, "git", "config", "user.name", "test")
	runCmd(t, tmpDir, "git", "config", "user.email", "test@test.com")

	remoteDir := t.TempDir()
	runCmd(t, remoteDir, "git", "init", "--bare", "-b", "main")
	runCmd(t, tmpDir, "git", "remote", "add", "origin", remoteDir)

	repo := RepoInfo{Path: tmpDir, RelPath: "test"}
	res := processSingleReset(repo, "main", "soft", nil)

	if !res.Skipped {
		t.Error("expected Skipped=true for no commits")
	}
	if res.SkipReason != "no commits" {
		t.Errorf("unexpected skip reason: %q", res.SkipReason)
	}
}

func TestProcessSingleResetSkipDetachedHEAD(t *testing.T) {
	repoDir, _ := makeRepoWithRemote(t)

	hash := strings.TrimSpace(string(runCmdOutput(t, repoDir, "git", "rev-parse", "HEAD")))
	runCmd(t, repoDir, "git", "checkout", hash)

	repo := RepoInfo{Path: repoDir, RelPath: "test"}
	res := processSingleReset(repo, "main", "soft", nil)

	if !res.Skipped {
		t.Error("expected Skipped=true for detached HEAD")
	}
	if res.SkipReason != "detached HEAD" {
		t.Errorf("unexpected skip reason: %q", res.SkipReason)
	}
}

func TestProcessSingleResetSkipBranchNotOnOrigin(t *testing.T) {
	repoDir, _ := makeRepoAheadOfOrigin(t)

	repo := RepoInfo{Path: repoDir, RelPath: "test"}
	res := processSingleReset(repo, "no-such-branch-xyz", "soft", nil)

	if !res.Skipped {
		t.Error("expected Skipped=true for branch not on origin")
	}
	if res.SkipReason != "branch not on origin" {
		t.Errorf("unexpected skip reason: %q", res.SkipReason)
	}
}

func TestProcessSingleResetSkipAlreadyAtTarget(t *testing.T) {
	repoDir, _ := makeRepoWithRemote(t)
	// local HEAD == origin/main — no local commits ahead

	repo := RepoInfo{Path: repoDir, RelPath: "test"}
	res := processSingleReset(repo, "main", "soft", nil)

	if !res.Skipped {
		t.Error("expected Skipped=true for already up to date")
	}
	if res.SkipReason != "already up to date" {
		t.Errorf("unexpected skip reason: %q", res.SkipReason)
	}
}

// --- processSingleReset: soft reset ---

func TestProcessSingleResetSoftMovesHEAD(t *testing.T) {
	repoDir, _ := makeRepoAheadOfOrigin(t)
	// local has one commit ahead of origin/main

	repo := RepoInfo{Path: repoDir, RelPath: "test"}
	res := processSingleReset(repo, "main", "soft", nil)

	if !res.Success {
		t.Fatalf("expected Success=true, got error: %q", res.Error)
	}

	// HEAD should now equal origin/main
	if !checkAlreadyAtTarget(repoDir, "main") {
		t.Error("expected HEAD to be at origin/main after soft reset")
	}

	// local.txt (from the local commit) should be staged
	stagedOut := runCmdOutput(t, repoDir, "git", "diff", "--cached", "--name-only")
	if !strings.Contains(string(stagedOut), "local.txt") {
		t.Errorf("expected local.txt staged after soft reset, got: %s", string(stagedOut))
	}
}

func TestProcessSingleResetSoftWarnsStagedChanges(t *testing.T) {
	repoDir, _ := makeRepoAheadOfOrigin(t)

	// Stage an extra change on top of the local commit
	writeFile(t, repoDir, "extra.txt", "extra staged content")
	runCmd(t, repoDir, "git", "add", "extra.txt")

	repo := RepoInfo{Path: repoDir, RelPath: "test"}
	res := processSingleReset(repo, "main", "soft", nil)

	if !res.Success {
		t.Fatalf("expected Success=true, got error: %q", res.Error)
	}
	if res.Warning == "" {
		t.Error("expected a warning about pre-existing staged changes")
	}
}

// --- processSingleReset: hard reset ---

func TestProcessSingleResetHardDiscardsChanges(t *testing.T) {
	repoDir, _ := makeRepoAheadOfOrigin(t)

	repo := RepoInfo{Path: repoDir, RelPath: "test"}
	res := processSingleReset(repo, "main", "hard", nil)

	if !res.Success {
		t.Fatalf("expected Success=true, got error: %q", res.Error)
	}

	// HEAD should be at origin/main
	if !checkAlreadyAtTarget(repoDir, "main") {
		t.Error("expected HEAD to be at origin/main after hard reset")
	}

	// local.txt was part of the local commit — it should be gone
	if _, err := os.Stat(filepath.Join(repoDir, "local.txt")); !os.IsNotExist(err) {
		t.Error("expected local.txt to be removed after hard reset")
	}
}

func TestProcessSingleResetHardSkipsMidMerge(t *testing.T) {
	repoDir, _ := makeRepoAheadOfOrigin(t)

	// Fake a mid-merge state
	mergeHead := filepath.Join(repoDir, ".git", "MERGE_HEAD")
	if err := os.WriteFile(mergeHead, []byte("abc123\n"), 0644); err != nil {
		t.Fatal(err)
	}

	repo := RepoInfo{Path: repoDir, RelPath: "test"}
	res := processSingleReset(repo, "main", "hard", nil)

	if !res.Skipped {
		t.Error("expected Skipped=true for mid-merge repo")
	}
	if !strings.Contains(res.SkipReason, "merge") {
		t.Errorf("expected skip reason to mention merge, got %q", res.SkipReason)
	}
}

// --- processSingleReset: rebase ---

func TestProcessSingleResetRebaseHappyPath(t *testing.T) {
	repoDir, _ := makeRepoAheadOfOrigin(t)
	// local = origin/main + one extra commit; rebase is a no-conflict fast-forward

	repo := RepoInfo{Path: repoDir, RelPath: "test"}
	res := processSingleReset(repo, "main", "rebase", nil)

	if !res.Success {
		t.Fatalf("expected Success=true, got error: %q", res.Error)
	}

	// No rebase in progress after success
	if checkRebaseInProgress(repoDir) {
		t.Error("expected no rebase dir after successful rebase")
	}
}

func TestProcessSingleResetRebaseFailsDirtyTree(t *testing.T) {
	repoDir, _ := makeRepoAheadOfOrigin(t)

	// Add an uncommitted/untracked file to make working tree dirty
	writeFile(t, repoDir, "dirty.txt", "dirty")

	repo := RepoInfo{Path: repoDir, RelPath: "test"}
	res := processSingleReset(repo, "main", "rebase", nil)

	if res.Success || res.Skipped {
		t.Error("expected failure for dirty working tree during rebase")
	}
	if res.Error != "working tree must be clean" {
		t.Errorf("expected 'working tree must be clean', got %q", res.Error)
	}
}

func TestProcessSingleResetRebaseSkipsIfInProgress(t *testing.T) {
	repoDir, _ := makeRepoAheadOfOrigin(t)

	// Fake a rebase-in-progress state
	rebaseMerge := filepath.Join(repoDir, ".git", "rebase-merge")
	if err := os.MkdirAll(rebaseMerge, 0755); err != nil {
		t.Fatal(err)
	}

	repo := RepoInfo{Path: repoDir, RelPath: "test"}
	res := processSingleReset(repo, "main", "rebase", nil)

	if !res.Skipped {
		t.Error("expected Skipped=true for rebase already in progress")
	}
	if res.SkipReason != "rebase already in progress" {
		t.Errorf("unexpected skip reason: %q", res.SkipReason)
	}
}

// --- processSingleReset: auto-switch to target branch ---

func TestProcessSingleResetSwitchesToTargetBranch(t *testing.T) {
	repoDir, _ := makeRepoWithRemote(t)

	// Create a "develop" branch, push it to origin so origin/develop exists.
	runCmd(t, repoDir, "git", "checkout", "-b", "develop")
	runCmd(t, repoDir, "git", "push", "origin", "develop")

	// Add a local commit on develop that has NOT been pushed — local is now
	// ahead of origin/develop, so the reset will actually execute.
	writeFile(t, repoDir, "local-develop.txt", "local develop content")
	runCmd(t, repoDir, "git", "add", ".")
	runCmd(t, repoDir, "git", "commit", "-m", "local develop commit ahead of origin")

	// Switch back to main so we're on a different branch.
	runCmd(t, repoDir, "git", "checkout", "main")

	// processSingleReset should auto-switch to "develop" then hard-reset it.
	repo := RepoInfo{Path: repoDir, RelPath: "test"}
	res := processSingleReset(repo, "develop", "hard", nil)

	if !res.Success {
		t.Fatalf("expected Success=true, got error: %q (skipped=%v reason=%q)", res.Error, res.Skipped, res.SkipReason)
	}

	current, err := getCurrentBranch(repoDir)
	if err != nil {
		t.Fatal(err)
	}
	if current != "develop" {
		t.Errorf("expected current branch 'develop', got %q", current)
	}

	// The local commit was discarded by hard reset, so the file must be gone.
	if _, err := os.Stat(filepath.Join(repoDir, "local-develop.txt")); !os.IsNotExist(err) {
		t.Error("expected local-develop.txt to be removed after hard reset")
	}
}

// --- processSingleReset: rebase conflict ---

func TestProcessSingleResetRebaseConflict(t *testing.T) {
	repoDir, _ := makeRepoWithConflict(t)
	// local main and origin/main diverge on conflict.txt → rebase must conflict

	repo := RepoInfo{Path: repoDir, RelPath: "test"}
	res := processSingleReset(repo, "main", "rebase", nil)

	if res.Success || res.Skipped {
		t.Errorf("expected failure for rebase conflict, got success=%v skipped=%v", res.Success, res.Skipped)
	}
	if !strings.Contains(res.Error, "conflict during rebase") {
		t.Errorf("expected conflict error, got %q", res.Error)
	}
	// Rebase should have been aborted — no lingering rebase state.
	if checkRebaseInProgress(repoDir) {
		t.Error("expected no rebase in progress after abort")
	}
}

// --- processSingleReset: fetch when already on branch (Bug 1 fix) ---

func TestProcessSingleResetFetchWhenAlreadyOnBranch(t *testing.T) {
	// Scenario: repoDir is on main and its local origin/main ref is stale (another
	// clone pushed a new commit to the remote that repoDir hasn't fetched yet).
	// processSingleReset should fetch first so the origin ref is fresh, then reset.
	remoteDir := t.TempDir()
	repoDir := t.TempDir()
	otherDir := t.TempDir()

	// Initial setup: bare remote + working repo, synced.
	runCmd(t, remoteDir, "git", "init", "--bare", "-b", "main")
	createGitRepo(t, repoDir)
	runCmd(t, repoDir, "git", "remote", "add", "origin", remoteDir)
	runCmd(t, repoDir, "git", "push", "origin", "main")
	runCmd(t, repoDir, "git", "fetch", "origin") // local origin/main ref = C0

	// Another clone pushes a new commit — repoDir does NOT fetch.
	runCmd(t, otherDir, "git", "init", "-b", "main")
	runCmd(t, otherDir, "git", "config", "user.name", "test")
	runCmd(t, otherDir, "git", "config", "user.email", "test@test.com")
	runCmd(t, otherDir, "git", "remote", "add", "origin", remoteDir)
	runCmd(t, otherDir, "git", "fetch", "origin")
	runCmd(t, otherDir, "git", "checkout", "-b", "main", "--track", "origin/main")
	writeFile(t, otherDir, "new-remote.txt", "new remote content")
	runCmd(t, otherDir, "git", "add", ".")
	runCmd(t, otherDir, "git", "commit", "-m", "new remote commit")
	runCmd(t, otherDir, "git", "push", "origin", "main")
	// repoDir's local origin/main ref is now stale (still C0, not the new push).

	repo := RepoInfo{Path: repoDir, RelPath: "test"}
	res := processSingleReset(repo, "main", "soft", nil)

	// Without the fetch fix, checkAlreadyAtTarget would compare HEAD(C0) == stale
	// origin/main(C0) and return true → skip. With the fix, fetch updates origin/main
	// to the new commit → not equal → soft reset runs → success.
	if !res.Success {
		t.Fatalf("expected Success=true (should fetch then reset), got error=%q skipped=%v reason=%q",
			res.Error, res.Skipped, res.SkipReason)
	}

	// HEAD should now be at the new remote commit.
	if !checkAlreadyAtTarget(repoDir, "main") {
		t.Error("expected HEAD to be at origin/main after soft reset")
	}
}

// --- syncBranch (soft mode, no TTY guard) ---

func TestSyncBranchSoft(t *testing.T) {
	tmpDir := t.TempDir()

	repoDir, _ := makeRepoAheadOfOrigin(t)
	// put repoDir inside tmpDir so findGitRepos finds it
	linkedDir := filepath.Join(tmpDir, "repo1")
	if err := os.Symlink(repoDir, linkedDir); err != nil {
		// Symlinks may not work on all CI — copy instead
		if err2 := os.MkdirAll(linkedDir, 0755); err2 != nil {
			t.Fatal(err2)
		}
		// Fall back: create a fresh repo directly inside tmpDir
		repoDir2, _ := makeRepoAheadOfOrigin(t)
		linkedDir = repoDir2
		tmpDir = filepath.Dir(repoDir2)
	}

	// Redirect stdout to suppress output
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	cfg := newConfig(defaultSkipDirs, nil, 20)
	syncBranch(tmpDir, "main", "soft", 2, cfg)

	_ = w.Close()
	os.Stdout = oldStdout

	buf := make([]byte, 4096)
	n, _ := r.Read(buf)
	output := string(buf[:n])
	_ = linkedDir

	if !strings.Contains(output, "Summary") {
		t.Errorf("expected summary in output, got: %s", output)
	}
}

// TestRunResetSoftFlag verifies that Run() parses -rs and dispatches to syncBranch.
// An empty directory is used so syncBranch returns early with "No repos found".
func TestRunResetSoftFlag(t *testing.T) {
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

	runErr := Run([]string{"-rs", "main"})

	_ = w.Close()
	os.Stdout = oldStdout

	if runErr != nil {
		t.Fatalf("expected no error from Run with -rs flag, got: %v", runErr)
	}
}

// TestSyncBranchSoftMixedOutcomes verifies the summary counts when some repos
// succeed and some are skipped (already up to date).
func TestSyncBranchSoftMixedOutcomes(t *testing.T) {
	tmpDir := t.TempDir()

	// repo-ahead: one local commit not pushed → soft reset will succeed
	remote1 := t.TempDir()
	repo1Dir := filepath.Join(tmpDir, "repo-ahead")
	if err := os.MkdirAll(repo1Dir, 0755); err != nil {
		t.Fatal(err)
	}
	runCmd(t, remote1, "git", "init", "--bare", "-b", "main")
	createGitRepo(t, repo1Dir)
	runCmd(t, repo1Dir, "git", "remote", "add", "origin", remote1)
	runCmd(t, repo1Dir, "git", "push", "origin", "main")
	runCmd(t, repo1Dir, "git", "fetch", "origin")
	writeFile(t, repo1Dir, "local.txt", "local")
	runCmd(t, repo1Dir, "git", "add", ".")
	runCmd(t, repo1Dir, "git", "commit", "-m", "local ahead")

	// repo-synced: exactly at origin/main → will be skipped
	remote2 := t.TempDir()
	repo2Dir := filepath.Join(tmpDir, "repo-synced")
	if err := os.MkdirAll(repo2Dir, 0755); err != nil {
		t.Fatal(err)
	}
	runCmd(t, remote2, "git", "init", "--bare", "-b", "main")
	createGitRepo(t, repo2Dir)
	runCmd(t, repo2Dir, "git", "remote", "add", "origin", remote2)
	runCmd(t, repo2Dir, "git", "push", "origin", "main")
	runCmd(t, repo2Dir, "git", "fetch", "origin")

	oldStdout := os.Stdout
	pr, pw, _ := os.Pipe()
	os.Stdout = pw

	cfg := newConfig(defaultSkipDirs, nil, 20)
	syncBranch(tmpDir, "main", "soft", 2, cfg)

	_ = pw.Close()
	os.Stdout = oldStdout

	outBytes, _ := io.ReadAll(pr)
	output := string(outBytes)

	if !strings.Contains(output, "1 succeeded") {
		t.Errorf("expected '1 succeeded' in output, got: %s", output)
	}
	if !strings.Contains(output, "1 skipped") {
		t.Errorf("expected '1 skipped' in output, got: %s", output)
	}
}
