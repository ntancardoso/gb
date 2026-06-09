package core

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func remoteTarget(remote, branch string) resetTarget {
	return resetTarget{isRemote: true, remote: remote, branch: branch}
}

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

func makeRepoAheadOfOrigin(t *testing.T) (repoDir, remoteDir string) {
	t.Helper()
	repoDir, remoteDir = makeRepoWithRemote(t)

	writeFile(t, repoDir, "local.txt", "local content")
	runCmd(t, repoDir, "git", "add", ".")
	runCmd(t, repoDir, "git", "commit", "-m", "local commit ahead of origin")
	return
}

func makeRepoWithConflict(t *testing.T) (repoDir, remoteDir string) {
	t.Helper()
	remoteDir = t.TempDir()
	repoDir = t.TempDir()
	otherDir := t.TempDir()

	runCmd(t, remoteDir, "git", "init", "--bare", "-b", "main")
	createGitRepo(t, repoDir)
	writeFile(t, repoDir, "conflict.txt", "base\n")
	runCmd(t, repoDir, "git", "add", "conflict.txt")
	runCmd(t, repoDir, "git", "commit", "-m", "add conflict.txt")
	runCmd(t, repoDir, "git", "remote", "add", "origin", remoteDir)
	runCmd(t, repoDir, "git", "push", "origin", "main")

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

	runCmd(t, repoDir, "git", "fetch", "origin")
	writeFile(t, repoDir, "conflict.txt", "local version\n")
	runCmd(t, repoDir, "git", "add", "conflict.txt")
	runCmd(t, repoDir, "git", "commit", "-m", "local conflicting change")

	return
}

func TestCheckRemoteExists(t *testing.T) {
	t.Run("no origin", func(t *testing.T) {
		tmpDir := t.TempDir()
		createGitRepo(t, tmpDir)
		if checkRemoteExists(tmpDir, "origin") {
			t.Error("expected no origin remote")
		}
	})

	t.Run("with origin", func(t *testing.T) {
		repoDir, _ := makeRepoWithRemote(t)
		if !checkRemoteExists(repoDir, "origin") {
			t.Error("expected origin remote to exist")
		}
	})
}

func TestResolveRemoteAndBranch(t *testing.T) {
	repoDir, remoteDir := makeRepoWithRemote(t)

	upstream := t.TempDir()
	runCmd(t, upstream, "git", "init", "--bare", "-b", "main")
	runCmd(t, repoDir, "git", "remote", "add", "upstream", upstream)

	_ = remoteDir

	cases := []struct {
		branchArg     string
		defaultRemote string
		wantRemote    string
		wantBranch    string
	}{
		{"main", "origin", "origin", "main"},
		{"main", "upstream", "upstream", "main"},

		{"origin/main", "origin", "origin", "main"},
		{"upstream/main", "origin", "upstream", "main"},

		{"origin/feat/branch1", "origin", "origin", "feat/branch1"},
		{"upstream/release/v1.0", "origin", "upstream", "release/v1.0"},

		{"feat/branch1", "origin", "origin", "feat/branch1"},
		{"feat/branch1", "upstream", "upstream", "feat/branch1"},
		{"release/v1.0", "origin", "origin", "release/v1.0"},

		{"noremote/main", "origin", "origin", "noremote/main"},
	}

	for _, tc := range cases {
		t.Run(tc.branchArg+"_default_"+tc.defaultRemote, func(t *testing.T) {
			gotRemote, gotBranch := resolveRemoteAndBranch(repoDir, tc.branchArg, tc.defaultRemote)
			if gotRemote != tc.wantRemote || gotBranch != tc.wantBranch {
				t.Errorf("resolveRemoteAndBranch(%q, %q) = (%q, %q), want (%q, %q)",
					tc.branchArg, tc.defaultRemote, gotRemote, gotBranch, tc.wantRemote, tc.wantBranch)
			}
		})
	}
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

func TestCheckBranchOnRemote(t *testing.T) {
	repoDir, _ := makeRepoWithRemote(t)

	t.Run("existing branch", func(t *testing.T) {
		found, err := checkBranchOnRemote(repoDir, "main", "origin")
		if err != nil {
			t.Fatal(err)
		}
		if !found {
			t.Error("expected main to be on origin")
		}
	})

	t.Run("non-existent branch", func(t *testing.T) {
		found, err := checkBranchOnRemote(repoDir, "does-not-exist-xyz", "origin")
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
		if !checkAlreadyAtTarget(repoDir, "origin/main") {
			t.Error("expected already at origin/main")
		}
	})

	t.Run("ahead of target", func(t *testing.T) {
		repoDir, _ := makeRepoAheadOfOrigin(t)
		if checkAlreadyAtTarget(repoDir, "origin/main") {
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

func TestProcessSingleResetSkipNoOrigin(t *testing.T) {
	tmpDir := t.TempDir()
	createGitRepo(t, tmpDir)

	repo := RepoInfo{Path: tmpDir, RelPath: "test"}
	res := processSingleReset(repo, remoteTarget("origin", "main"), "soft", nil)

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
	res := processSingleReset(repo, remoteTarget("origin", "main"), "soft", nil)

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
	res := processSingleReset(repo, remoteTarget("origin", "main"), "soft", nil)

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
	res := processSingleReset(repo, remoteTarget("origin", "no-such-branch-xyz"), "soft", nil)

	if !res.Skipped {
		t.Error("expected Skipped=true for branch not on origin")
	}
	if res.SkipReason != "branch not on origin" {
		t.Errorf("unexpected skip reason: %q", res.SkipReason)
	}
}

func TestProcessSingleResetSkipAlreadyAtTarget(t *testing.T) {
	repoDir, _ := makeRepoWithRemote(t)

	repo := RepoInfo{Path: repoDir, RelPath: "test"}
	res := processSingleReset(repo, remoteTarget("origin", "main"), "soft", nil)

	if !res.Skipped {
		t.Error("expected Skipped=true for already up to date")
	}
	if res.SkipReason != "already up to date" {
		t.Errorf("unexpected skip reason: %q", res.SkipReason)
	}
}

func TestProcessSingleResetSoftMovesHEAD(t *testing.T) {
	repoDir, _ := makeRepoAheadOfOrigin(t)

	repo := RepoInfo{Path: repoDir, RelPath: "test"}
	res := processSingleReset(repo, remoteTarget("origin", "main"), "soft", nil)

	if !res.Success {
		t.Fatalf("expected Success=true, got error: %q", res.Error)
	}

	if !checkAlreadyAtTarget(repoDir, "origin/main") {
		t.Error("expected HEAD to be at origin/main after soft reset")
	}

	stagedOut := runCmdOutput(t, repoDir, "git", "diff", "--cached", "--name-only")
	if !strings.Contains(string(stagedOut), "local.txt") {
		t.Errorf("expected local.txt staged after soft reset, got: %s", string(stagedOut))
	}
}

func TestProcessSingleResetSoftWarnsStagedChanges(t *testing.T) {
	repoDir, _ := makeRepoAheadOfOrigin(t)

	writeFile(t, repoDir, "extra.txt", "extra staged content")
	runCmd(t, repoDir, "git", "add", "extra.txt")

	repo := RepoInfo{Path: repoDir, RelPath: "test"}
	res := processSingleReset(repo, remoteTarget("origin", "main"), "soft", nil)

	if !res.Success {
		t.Fatalf("expected Success=true, got error: %q", res.Error)
	}
	if res.Warning == "" {
		t.Error("expected a warning about pre-existing staged changes")
	}
}

func TestProcessSingleResetHardDiscardsChanges(t *testing.T) {
	repoDir, _ := makeRepoAheadOfOrigin(t)

	repo := RepoInfo{Path: repoDir, RelPath: "test"}
	res := processSingleReset(repo, remoteTarget("origin", "main"), "hard", nil)

	if !res.Success {
		t.Fatalf("expected Success=true, got error: %q", res.Error)
	}

	if !checkAlreadyAtTarget(repoDir, "origin/main") {
		t.Error("expected HEAD to be at origin/main after hard reset")
	}

	if _, err := os.Stat(filepath.Join(repoDir, "local.txt")); !os.IsNotExist(err) {
		t.Error("expected local.txt to be removed after hard reset")
	}
}

func TestProcessSingleResetHardSkipsMidMerge(t *testing.T) {
	repoDir, _ := makeRepoAheadOfOrigin(t)

	mergeHead := filepath.Join(repoDir, ".git", "MERGE_HEAD")
	if err := os.WriteFile(mergeHead, []byte("abc123\n"), 0644); err != nil {
		t.Fatal(err)
	}

	repo := RepoInfo{Path: repoDir, RelPath: "test"}
	res := processSingleReset(repo, remoteTarget("origin", "main"), "hard", nil)

	if !res.Skipped {
		t.Error("expected Skipped=true for mid-merge repo")
	}
	if !strings.Contains(res.SkipReason, "merge") {
		t.Errorf("expected skip reason to mention merge, got %q", res.SkipReason)
	}
}

func TestProcessSingleResetRebaseHappyPath(t *testing.T) {
	repoDir, _ := makeRepoAheadOfOrigin(t)

	repo := RepoInfo{Path: repoDir, RelPath: "test"}
	res := processSingleReset(repo, remoteTarget("origin", "main"), "rebase", nil)

	if !res.Success {
		t.Fatalf("expected Success=true, got error: %q", res.Error)
	}

	if checkRebaseInProgress(repoDir) {
		t.Error("expected no rebase dir after successful rebase")
	}
}

func TestProcessSingleResetRebaseFailsDirtyTree(t *testing.T) {
	repoDir, _ := makeRepoAheadOfOrigin(t)

	writeFile(t, repoDir, "dirty.txt", "dirty")

	repo := RepoInfo{Path: repoDir, RelPath: "test"}
	res := processSingleReset(repo, remoteTarget("origin", "main"), "rebase", nil)

	if res.Success || res.Skipped {
		t.Error("expected failure for dirty working tree during rebase")
	}
	if res.Error != "working tree must be clean" {
		t.Errorf("expected 'working tree must be clean', got %q", res.Error)
	}
}

func TestProcessSingleResetRebaseSkipsIfInProgress(t *testing.T) {
	repoDir, _ := makeRepoAheadOfOrigin(t)

	rebaseMerge := filepath.Join(repoDir, ".git", "rebase-merge")
	if err := os.MkdirAll(rebaseMerge, 0755); err != nil {
		t.Fatal(err)
	}

	repo := RepoInfo{Path: repoDir, RelPath: "test"}
	res := processSingleReset(repo, remoteTarget("origin", "main"), "rebase", nil)

	if !res.Skipped {
		t.Error("expected Skipped=true for rebase already in progress")
	}
	if res.SkipReason != "rebase already in progress" {
		t.Errorf("unexpected skip reason: %q", res.SkipReason)
	}
}

func TestProcessSingleResetStaysOnCurrentBranch(t *testing.T) {
	repoDir, remoteDir := makeRepoWithRemote(t)

	otherDir := t.TempDir()
	runCmd(t, otherDir, "git", "init", "-b", "main")
	runCmd(t, otherDir, "git", "config", "user.name", "test")
	runCmd(t, otherDir, "git", "config", "user.email", "test@test.com")
	runCmd(t, otherDir, "git", "remote", "add", "origin", remoteDir)
	runCmd(t, otherDir, "git", "fetch", "origin")
	runCmd(t, otherDir, "git", "checkout", "-b", "main", "--track", "origin/main")
	writeFile(t, otherDir, "from-origin.txt", "new content on origin")
	runCmd(t, otherDir, "git", "add", ".")
	runCmd(t, otherDir, "git", "commit", "-m", "new origin commit")
	runCmd(t, otherDir, "git", "push", "origin", "main")

	runCmd(t, repoDir, "git", "checkout", "-b", "feat/my-task")

	repo := RepoInfo{Path: repoDir, RelPath: "test"}
	res := processSingleReset(repo, remoteTarget("origin", "main"), "hard", nil)

	if !res.Success {
		t.Fatalf("expected Success=true, got error: %q (skipped=%v reason=%q)", res.Error, res.Skipped, res.SkipReason)
	}

	current, err := getCurrentBranch(repoDir)
	if err != nil {
		t.Fatal(err)
	}
	if current != "feat/my-task" {
		t.Errorf("expected current branch to remain 'feat/my-task', got %q", current)
	}

	if _, statErr := os.Stat(filepath.Join(repoDir, "from-origin.txt")); os.IsNotExist(statErr) {
		t.Error("expected from-origin.txt to exist after hard reset to origin/main")
	}
}

func TestProcessSingleResetRebaseConflict(t *testing.T) {
	repoDir, _ := makeRepoWithConflict(t)

	repo := RepoInfo{Path: repoDir, RelPath: "test"}
	res := processSingleReset(repo, remoteTarget("origin", "main"), "rebase", nil)

	if res.Success || res.Skipped {
		t.Errorf("expected failure for rebase conflict, got success=%v skipped=%v", res.Success, res.Skipped)
	}
	if !strings.Contains(res.Error, "conflict during rebase") {
		t.Errorf("expected conflict error, got %q", res.Error)
	}
	if checkRebaseInProgress(repoDir) {
		t.Error("expected no rebase in progress after abort")
	}
}

func TestProcessSingleResetFetchWhenAlreadyOnBranch(t *testing.T) {
	remoteDir := t.TempDir()
	repoDir := t.TempDir()
	otherDir := t.TempDir()

	runCmd(t, remoteDir, "git", "init", "--bare", "-b", "main")
	createGitRepo(t, repoDir)
	runCmd(t, repoDir, "git", "remote", "add", "origin", remoteDir)
	runCmd(t, repoDir, "git", "push", "origin", "main")
	runCmd(t, repoDir, "git", "fetch", "origin")

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

	repo := RepoInfo{Path: repoDir, RelPath: "test"}
	res := processSingleReset(repo, remoteTarget("origin", "main"), "soft", nil)

	if !res.Success {
		t.Fatalf("expected Success=true (should fetch then reset), got error=%q skipped=%v reason=%q",
			res.Error, res.Skipped, res.SkipReason)
	}

	if !checkAlreadyAtTarget(repoDir, "origin/main") {
		t.Error("expected HEAD to be at origin/main after soft reset")
	}
}

func TestSyncBranchSoft(t *testing.T) {
	tmpDir := t.TempDir()

	remote := t.TempDir()
	repoDir := filepath.Join(tmpDir, "repo1")
	runCmd(t, remote, "git", "init", "--bare", "-b", "main")
	createGitRepo(t, repoDir)
	runCmd(t, repoDir, "git", "remote", "add", "origin", remote)
	runCmd(t, repoDir, "git", "push", "origin", "main")
	runCmd(t, repoDir, "git", "fetch", "origin")
	writeFile(t, repoDir, "local.txt", "local content")
	runCmd(t, repoDir, "git", "add", ".")
	runCmd(t, repoDir, "git", "commit", "-m", "local commit ahead of origin")

	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	cfg := mustConfig(t, defaultExcludeDirs, nil, nil, nil, 20, false, "origin")
	syncBranch(context.Background(), tmpDir, "origin/main", "", "soft", 2, cfg) //nolint:errcheck

	_ = w.Close()
	os.Stdout = oldStdout

	outBytes, _ := io.ReadAll(r)
	output := string(outBytes)

	if !strings.Contains(output, "Summary") {
		t.Errorf("expected summary in output, got: %s", output)
	}
}

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

	runErr := Run(context.Background(), []string{"-rs", "main"})

	_ = w.Close()
	os.Stdout = oldStdout

	if runErr != nil {
		t.Fatalf("expected no error from Run with -rs flag, got: %v", runErr)
	}
}

func TestSyncBranchSoftMixedOutcomes(t *testing.T) {
	tmpDir := t.TempDir()

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

	cfg := mustConfig(t, defaultExcludeDirs, nil, nil, nil, 20, false, "origin")
	syncBranch(context.Background(), tmpDir, "origin/main", "", "soft", 2, cfg) //nolint:errcheck

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

func TestValidateResetArgs(t *testing.T) {
	cases := []struct {
		name           string
		flagVal        string
		posBranch      string
		nargs          int
		remoteExplicit bool
		wantErr        bool
	}{
		{"bare branch", "main", "", 0, false, false},
		{"inline remote", "origin/main", "", 0, false, false},
		{"two-token", "origin", "main", 1, false, false},
		{"explicit -r", "main", "", 0, true, false},
		{"slashed branch with -r", "feat/x", "", 0, true, true},
		{"inline prefix with -r", "origin/main", "", 0, true, true},
		{"surplus positionals", "main", "extra", 2, false, true},
		{"two-token plus -r", "origin", "main", 1, true, true},
		{"two-token slashed remote", "origin/main", "main", 1, false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateResetArgs(tc.flagVal, tc.posBranch, tc.nargs, tc.remoteExplicit)
			if (err != nil) != tc.wantErr {
				t.Errorf("validateResetArgs(%q,%q,%d,%v) err=%v, wantErr=%v",
					tc.flagVal, tc.posBranch, tc.nargs, tc.remoteExplicit, err, tc.wantErr)
			}
		})
	}
}

func TestResolveResetTarget(t *testing.T) {
	repoDir, _ := makeRepoWithRemote(t)

	upstream := t.TempDir()
	runCmd(t, upstream, "git", "init", "--bare", "-b", "main")
	runCmd(t, repoDir, "git", "remote", "add", "upstream", upstream)

	origin2 := t.TempDir()
	runCmd(t, origin2, "git", "init", "--bare", "-b", "main")
	runCmd(t, repoDir, "git", "remote", "add", "origin2", origin2)

	cases := []struct {
		name       string
		arg        string
		posBranch  string
		flagRemote string
		flagSet    bool
		want       resetTarget
	}{
		{"bare branch is local", "main", "", "origin", false, resetTarget{isRemote: false, branch: "main", fallbackRemote: "origin"}},
		{"inline origin", "origin/main", "", "origin", false, resetTarget{isRemote: true, remote: "origin", branch: "main"}},
		{"inline upstream", "upstream/main", "", "origin", false, resetTarget{isRemote: true, remote: "upstream", branch: "main"}},
		{"inline origin2", "origin2/main", "", "origin", false, resetTarget{isRemote: true, remote: "origin2", branch: "main"}},
		{"inline slashed branch", "origin/feat/x", "", "origin", false, resetTarget{isRemote: true, remote: "origin", branch: "feat/x"}},
		{"local slashed branch", "feat/x", "", "origin", false, resetTarget{isRemote: false, branch: "feat/x", fallbackRemote: "origin"}},
		{"unknown remote prefix stays local", "noremote/main", "", "origin", false, resetTarget{isRemote: false, branch: "noremote/main", fallbackRemote: "origin"}},
		{"two-token", "origin", "main", "origin", false, resetTarget{isRemote: true, remote: "origin", branch: "main"}},
		{"two-token slashed branch", "origin", "feat/x", "origin", false, resetTarget{isRemote: true, remote: "origin", branch: "feat/x"}},
		{"explicit -r forces remote", "main", "", "upstream", true, resetTarget{isRemote: true, remote: "upstream", branch: "main"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := resolveResetTarget(repoDir, tc.arg, tc.posBranch, tc.flagRemote, tc.flagSet)
			if got != tc.want {
				t.Errorf("resolveResetTarget(%q, %q, %q, %v) = %+v, want %+v",
					tc.arg, tc.posBranch, tc.flagRemote, tc.flagSet, got, tc.want)
			}
		})
	}
}

func TestProcessSingleResetLocalHardNoRemote(t *testing.T) {
	repoDir := t.TempDir()
	createGitRepo(t, repoDir)

	runCmd(t, repoDir, "git", "branch", "safe")
	writeFile(t, repoDir, "throwaway.txt", "discard me")
	runCmd(t, repoDir, "git", "add", ".")
	runCmd(t, repoDir, "git", "commit", "-m", "commit to be discarded")

	safeHash := strings.TrimSpace(string(runCmdOutput(t, repoDir, "git", "rev-parse", "safe")))

	repo := RepoInfo{Path: repoDir, RelPath: "test"}
	res := processSingleReset(repo, resetTarget{branch: "safe"}, "hard", nil)

	if !res.Success {
		t.Fatalf("expected Success=true for local hard reset, got error=%q skipped=%v reason=%q", res.Error, res.Skipped, res.SkipReason)
	}
	headHash := strings.TrimSpace(string(runCmdOutput(t, repoDir, "git", "rev-parse", "HEAD")))
	if headHash != safeHash {
		t.Errorf("expected HEAD at safe (%s), got %s", safeHash, headHash)
	}
	if _, statErr := os.Stat(filepath.Join(repoDir, "throwaway.txt")); !os.IsNotExist(statErr) {
		t.Error("expected throwaway.txt to be removed after local hard reset")
	}
}

func TestProcessSingleResetLocalFallbackToRemote(t *testing.T) {
	repoDir, remoteDir := makeRepoWithRemote(t)

	otherDir := t.TempDir()
	runCmd(t, otherDir, "git", "init", "-b", "main")
	runCmd(t, otherDir, "git", "config", "user.name", "test")
	runCmd(t, otherDir, "git", "config", "user.email", "test@test.com")
	runCmd(t, otherDir, "git", "remote", "add", "origin", remoteDir)
	runCmd(t, otherDir, "git", "fetch", "origin")
	runCmd(t, otherDir, "git", "checkout", "-b", "main", "--track", "origin/main")
	runCmd(t, otherDir, "git", "checkout", "-b", "only-on-remote")
	writeFile(t, otherDir, "remote-feature.txt", "remote feature content")
	runCmd(t, otherDir, "git", "add", ".")
	runCmd(t, otherDir, "git", "commit", "-m", "remote-only feature commit")
	runCmd(t, otherDir, "git", "push", "origin", "only-on-remote")

	repo := RepoInfo{Path: repoDir, RelPath: "test"}
	res := processSingleReset(repo, resetTarget{branch: "only-on-remote", fallbackRemote: "origin"}, "hard", nil)

	if !res.Success {
		t.Fatalf("expected Success=true via remote fallback, got error=%q skipped=%v reason=%q", res.Error, res.Skipped, res.SkipReason)
	}
	if !checkAlreadyAtTarget(repoDir, "origin/only-on-remote") {
		t.Error("expected HEAD to be at origin/only-on-remote after fallback reset")
	}
	if _, statErr := os.Stat(filepath.Join(repoDir, "remote-feature.txt")); os.IsNotExist(statErr) {
		t.Error("expected remote-feature.txt to exist after fallback reset")
	}
}

func TestProcessSingleResetLocalMissingEverywhere(t *testing.T) {
	repoDir, _ := makeRepoWithRemote(t)

	repo := RepoInfo{Path: repoDir, RelPath: "test"}
	res := processSingleReset(repo, resetTarget{branch: "ghost-branch", fallbackRemote: "origin"}, "hard", nil)

	if !res.Skipped {
		t.Fatalf("expected skip when branch is absent locally and on remote, got success=%v error=%q", res.Success, res.Error)
	}
	if !strings.Contains(res.SkipReason, "not found locally or on origin") {
		t.Errorf("unexpected skip reason: %q", res.SkipReason)
	}
}

func TestSyncBranchDisplayNoDoubleRemotePrefix(t *testing.T) {
	tmpDir := t.TempDir()
	remote := t.TempDir()
	repoDir := filepath.Join(tmpDir, "repo1")
	runCmd(t, remote, "git", "init", "--bare", "-b", "main")
	createGitRepo(t, repoDir)
	runCmd(t, repoDir, "git", "remote", "add", "origin", remote)
	runCmd(t, repoDir, "git", "push", "origin", "main")
	runCmd(t, repoDir, "git", "fetch", "origin")
	writeFile(t, repoDir, "local.txt", "local content")
	runCmd(t, repoDir, "git", "add", ".")
	runCmd(t, repoDir, "git", "commit", "-m", "local commit ahead of origin")

	oldStdout := os.Stdout
	pr, pw, _ := os.Pipe()
	os.Stdout = pw

	cfg := mustConfig(t, defaultExcludeDirs, nil, nil, nil, 20, false, "origin")
	syncBranch(context.Background(), tmpDir, "origin/main", "", "soft", 2, cfg) //nolint:errcheck

	_ = pw.Close()
	os.Stdout = oldStdout

	outBytes, _ := io.ReadAll(pr)
	output := string(outBytes)

	if strings.Contains(output, "origin/origin/main") {
		t.Errorf("output must not contain doubled remote prefix, got: %s", output)
	}
	if !strings.Contains(output, "git reset --soft origin/main") {
		t.Errorf("expected 'git reset --soft origin/main' in output, got: %s", output)
	}
}
