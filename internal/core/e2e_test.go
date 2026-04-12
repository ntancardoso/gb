package core

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func makeRepoOnFeatureBranchWithStaleOriginMain(t *testing.T) (repoDir, remoteDir string) {
	t.Helper()
	remoteDir = t.TempDir()
	repoDir = t.TempDir()
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
	writeFile(t, otherDir, "remote-only.txt", "pushed by other clone")
	runCmd(t, otherDir, "git", "add", ".")
	runCmd(t, otherDir, "git", "commit", "-m", "C1: remote-only commit")
	runCmd(t, otherDir, "git", "push", "origin", "main")

	runCmd(t, repoDir, "git", "checkout", "-b", "feature")

	return
}

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

	branch, err := getCurrentBranch(repoDir)
	if err != nil {
		t.Fatal(err)
	}
	if branch != "feature" {
		t.Errorf("expected current branch to remain 'feature', got %q", branch)
	}

	if _, statErr := os.Stat(filepath.Join(repoDir, "remote-only.txt")); os.IsNotExist(statErr) {
		t.Error("expected remote-only.txt to exist after hard reset to origin/main")
	}
}

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

func makeRepoWithCustomRemote(t *testing.T, remoteName string) (repoDir, remoteDir string) {
	t.Helper()
	remoteDir = t.TempDir()
	repoDir = t.TempDir()

	runCmd(t, remoteDir, "git", "init", "--bare", "-b", "main")
	createGitRepo(t, repoDir)
	runCmd(t, repoDir, "git", "remote", "add", remoteName, remoteDir)
	runCmd(t, repoDir, "git", "push", remoteName, "main")
	runCmd(t, repoDir, "git", "fetch", remoteName)

	writeFile(t, repoDir, "local-only.txt", "local commit ahead of remote")
	runCmd(t, repoDir, "git", "add", ".")
	runCmd(t, repoDir, "git", "commit", "-m", "local commit ahead of upstream/main")
	return
}

func TestE2ECustomRemoteProcessSingleReset(t *testing.T) {
	repoDir, _ := makeRepoWithCustomRemote(t, "upstream")

	repo := RepoInfo{Path: repoDir, RelPath: "repo"}
	resOrigin := processSingleReset(repo, "main", "soft", "origin", nil)
	if !resOrigin.Skipped {
		t.Errorf("expected skip when using non-existent remote 'origin', got success=%v error=%q", resOrigin.Success, resOrigin.Error)
	}
	if !strings.Contains(resOrigin.SkipReason, "origin") {
		t.Errorf("expected skip reason to mention 'origin', got %q", resOrigin.SkipReason)
	}

	resUpstream := processSingleReset(repo, "main", "soft", "upstream", nil)
	if resUpstream.Skipped {
		t.Fatalf("expected reset to run with remote 'upstream', got skipped: %q", resUpstream.SkipReason)
	}
	if !resUpstream.Success {
		t.Fatalf("expected Success=true with remote 'upstream', got error: %q", resUpstream.Error)
	}
}

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

	writeFile(t, repoDir, "ahead.txt", "ahead of upstream")
	runCmd(t, repoDir, "git", "add", ".")
	runCmd(t, repoDir, "git", "commit", "-m", "local commit ahead of upstream")

	oldStdout := os.Stdout
	pr, pw, _ := os.Pipe()
	os.Stdout = pw

	cfg := mustConfig(t, defaultExcludeDirs, nil, nil, nil, 20, false, "upstream")
	syncBranch(context.Background(), tmpDir, "main", "soft", 2, cfg) //nolint:errcheck

	_ = pw.Close()
	os.Stdout = oldStdout

	outBytes, _ := io.ReadAll(pr)
	output := string(outBytes)

	if !strings.Contains(output, "1 succeeded") {
		t.Errorf("expected '1 succeeded' using remote 'upstream', got: %s", output)
	}
}

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

	runErr := Run(context.Background(), []string{"-rs", "main", "-r", "upstream"})

	_ = w.Close()
	os.Stdout = oldStdout

	if runErr != nil {
		t.Fatalf("Run() with -r upstream returned error: %v", runErr)
	}
}

func TestE2EInlineRemoteReset(t *testing.T) {
	repoDir, remoteDir := makeRepoWithRemote(t)

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

	res := processSingleReset(repo, "origin/main", "hard", "origin", nil)
	if !res.Success {
		t.Fatalf("expected Success=true, got error=%q skipped=%v reason=%q", res.Error, res.Skipped, res.SkipReason)
	}
	if _, statErr := os.Stat(filepath.Join(repoDir, "inline-remote.txt")); os.IsNotExist(statErr) {
		t.Error("expected inline-remote.txt to exist after reset to origin/main")
	}
}

func TestE2EInlineRemoteResetSlashedBranch(t *testing.T) {
	repoDir, remoteDir := makeRepoWithRemote(t)

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

func TestWorktreeCreateRemoteOnlyBase(t *testing.T) {
	remoteDir := t.TempDir()
	parentDir := t.TempDir()
	repoDir := filepath.Join(parentDir, "projA")

	runCmd(t, remoteDir, "git", "init", "--bare", "-b", "main")
	createGitRepo(t, repoDir)
	runCmd(t, repoDir, "git", "remote", "add", "origin", remoteDir)
	runCmd(t, repoDir, "git", "push", "origin", "main")
	runCmd(t, repoDir, "git", "fetch", "origin")

	runCmd(t, repoDir, "git", "checkout", "--detach")
	runCmd(t, repoDir, "git", "branch", "-D", "main")

	cfg := mustConfig(t, defaultExcludeDirs, nil, nil, nil, 20, false, "origin")
	worktreeCreate(context.Background(), parentDir, "feat/my-task", "main", 2, cfg) //nolint:errcheck

	wtPath := filepath.Join(parentDir, "projA-my-task")
	if _, err := os.Stat(wtPath); err != nil {
		t.Fatalf("expected worktree at %s, got: %v", wtPath, err)
	}
}

func TestHardResetRunsWhenAlreadyAtTarget(t *testing.T) {
	repoDir, _ := makeRepoWithRemote(t)

	writeFile(t, repoDir, "README.md", "modified content — should be discarded")

	repo := RepoInfo{Path: repoDir, RelPath: "repo"}
	res := processSingleReset(repo, "main", "hard", "origin", nil)

	if res.Skipped {
		t.Fatalf("hard reset must not be skipped even when HEAD == origin/main; got skipped: %q", res.SkipReason)
	}
	if !res.Success {
		t.Fatalf("expected Success=true, got error: %q", res.Error)
	}

	content, err := os.ReadFile(filepath.Join(repoDir, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(content), "modified content") {
		t.Error("expected hard reset to restore README.md to committed content")
	}
}

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

func makeMultiRepoWorkspace(t *testing.T) (parentDir, repoA, repoB string) {
	t.Helper()
	parentDir = t.TempDir()
	repoA = filepath.Join(parentDir, "repoA")
	repoB = filepath.Join(parentDir, "repoB")
	createGitRepo(t, repoA)
	createGitRepo(t, repoB)
	return
}

func TestE2EWorktreeRemoveGlobMultiRepo(t *testing.T) {
	parentDir, repoA, repoB := makeMultiRepoWorkspace(t)

	for _, repo := range []string{repoA, repoB} {
		runCmd(t, repo, "git", "branch", "feat/AB-100")
		runCmd(t, repo, "git", "branch", "feat/AB-200")
		runCmd(t, repo, "git", "branch", "fix/bug-1")
	}

	wtA100 := filepath.Join(parentDir, "repoA-AB-100")
	wtA200 := filepath.Join(parentDir, "repoA-AB-200")
	wtAFix := filepath.Join(parentDir, "repoA-bug-1")
	wtB100 := filepath.Join(parentDir, "repoB-AB-100")
	wtB200 := filepath.Join(parentDir, "repoB-AB-200")
	wtBFix := filepath.Join(parentDir, "repoB-bug-1")

	runCmd(t, repoA, "git", "worktree", "add", wtA100, "feat/AB-100")
	runCmd(t, repoA, "git", "worktree", "add", wtA200, "feat/AB-200")
	runCmd(t, repoA, "git", "worktree", "add", wtAFix, "fix/bug-1")
	runCmd(t, repoB, "git", "worktree", "add", wtB100, "feat/AB-100")
	runCmd(t, repoB, "git", "worktree", "add", wtB200, "feat/AB-200")
	runCmd(t, repoB, "git", "worktree", "add", wtBFix, "fix/bug-1")

	cfg := mustConfig(t, defaultExcludeDirs, nil, nil, nil, 20, false, "origin")
	worktreeRemove(context.Background(), parentDir, "feat/AB*", 2, cfg) //nolint:errcheck

	for _, wt := range []string{wtA100, wtA200, wtB100, wtB200} {
		if _, err := os.Stat(wt); err == nil {
			t.Errorf("expected worktree %s to be removed", wt)
		}
	}
	for _, wt := range []string{wtAFix, wtBFix} {
		if _, err := os.Stat(wt); err != nil {
			t.Errorf("expected worktree %s to remain, got: %v", wt, err)
		}
	}
}

func TestE2EWorktreeRemoveGlobViaRunFlag(t *testing.T) {
	parentDir, repoA, repoB := makeMultiRepoWorkspace(t)

	for _, repo := range []string{repoA, repoB} {
		runCmd(t, repo, "git", "branch", "feat/task-1")
		runCmd(t, repo, "git", "branch", "fix/unrelated")
	}

	wtA := filepath.Join(parentDir, "repoA-task-1")
	wtB := filepath.Join(parentDir, "repoB-task-1")
	wtAFix := filepath.Join(parentDir, "repoA-unrelated")
	wtBFix := filepath.Join(parentDir, "repoB-unrelated")

	runCmd(t, repoA, "git", "worktree", "add", wtA, "feat/task-1")
	runCmd(t, repoA, "git", "worktree", "add", wtAFix, "fix/unrelated")
	runCmd(t, repoB, "git", "worktree", "add", wtB, "feat/task-1")
	runCmd(t, repoB, "git", "worktree", "add", wtBFix, "fix/unrelated")

	origDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(parentDir); err != nil {
		t.Skip("cannot change directory:", err)
	}
	defer func() { _ = os.Chdir(origDir) }()

	oldStdout := os.Stdout
	_, w, _ := os.Pipe()
	os.Stdout = w
	runErr := Run(context.Background(), []string{"-wr", "feat/*"})
	_ = w.Close()
	os.Stdout = oldStdout

	if runErr != nil {
		t.Fatalf("Run(-wr feat/*) returned error: %v", runErr)
	}

	for _, wt := range []string{wtA, wtB} {
		if _, err := os.Stat(wt); err == nil {
			t.Errorf("expected worktree %s to be removed after -wr feat/*", wt)
		}
	}
	for _, wt := range []string{wtAFix, wtBFix} {
		if _, err := os.Stat(wt); err != nil {
			t.Errorf("expected worktree %s to remain, got: %v", wt, err)
		}
	}
}

func TestE2EWorktreeIbFilterRemove(t *testing.T) {
	parentDir, repoA, repoB := makeMultiRepoWorkspace(t)

	runCmd(t, repoB, "git", "checkout", "-b", "develop")

	for _, repo := range []string{repoA, repoB} {
		runCmd(t, repo, "git", "branch", "feat/task-1")
	}

	wtA := filepath.Join(parentDir, "repoA-task-1")
	wtB := filepath.Join(parentDir, "repoB-task-1")
	runCmd(t, repoA, "git", "worktree", "add", wtA, "feat/task-1")
	runCmd(t, repoB, "git", "worktree", "add", wtB, "feat/task-1")

	cfg := mustConfig(t, defaultExcludeDirs, nil, nil, []string{"main"}, 20, false, "origin")
	worktreeRemove(context.Background(), parentDir, "feat/task-1", 2, cfg) //nolint:errcheck

	if _, err := os.Stat(wtA); err == nil {
		t.Errorf("expected repoA worktree to be removed (repoA is on main)")
	}
	if _, err := os.Stat(wtB); err != nil {
		t.Errorf("expected repoB worktree to remain (repoB is on develop), got: %v", err)
	}
}

func TestE2EWorktreeIbGlobFilterRemove(t *testing.T) {
	parentDir, repoA, repoB := makeMultiRepoWorkspace(t)

	runCmd(t, repoA, "git", "checkout", "-b", "feat/develop")

	for _, repo := range []string{repoA, repoB} {
		runCmd(t, repo, "git", "branch", "task/cleanup")
	}

	wtA := filepath.Join(parentDir, "repoA-cleanup")
	wtB := filepath.Join(parentDir, "repoB-cleanup")
	runCmd(t, repoA, "git", "worktree", "add", wtA, "task/cleanup")
	runCmd(t, repoB, "git", "worktree", "add", wtB, "task/cleanup")

	cfg := mustConfig(t, defaultExcludeDirs, nil, nil, []string{"feat/*"}, 20, false, "origin")
	worktreeRemove(context.Background(), parentDir, "task/cleanup", 2, cfg) //nolint:errcheck

	if _, err := os.Stat(wtA); err == nil {
		t.Errorf("expected repoA worktree to be removed (repoA is on feat/develop which matches feat/*)")
	}
	if _, err := os.Stat(wtB); err != nil {
		t.Errorf("expected repoB worktree to remain (repoB is on main, no match for feat/*), got: %v", err)
	}
}

func TestE2EWorktreeCommandsInLinkedWorktreeWorkspace(t *testing.T) {
	mainDir := t.TempDir()
	featDir := t.TempDir()

	repoA := filepath.Join(mainDir, "repoA")
	repoB := filepath.Join(mainDir, "repoB")
	createGitRepo(t, repoA)
	createGitRepo(t, repoB)

	for _, repo := range []string{repoA, repoB} {
		runCmd(t, repo, "git", "branch", "feat/task")
	}
	wtA := filepath.Join(featDir, "repoA")
	wtB := filepath.Join(featDir, "repoB")
	runCmd(t, repoA, "git", "worktree", "add", wtA, "feat/task")
	runCmd(t, repoB, "git", "worktree", "add", wtB, "feat/task")

	cfg := mustConfig(t, defaultExcludeDirs, nil, nil, nil, 20, false, "origin")

	worktreeListAll(context.Background(), featDir, 2, cfg) //nolint:errcheck

	for _, repo := range []string{repoA, repoB} {
		runCmd(t, repo, "git", "branch", "feat/cleanup")
	}
	wtACleanup := filepath.Join(mainDir, "repoA-cleanup")
	wtBCleanup := filepath.Join(mainDir, "repoB-cleanup")
	runCmd(t, repoA, "git", "worktree", "add", wtACleanup, "feat/cleanup")
	runCmd(t, repoB, "git", "worktree", "add", wtBCleanup, "feat/cleanup")

	worktreeRemove(context.Background(), featDir, "feat/cleanup", 2, cfg) //nolint:errcheck

	if _, err := os.Stat(wtACleanup); err == nil {
		t.Errorf("expected worktree %s to be removed via linked-worktree workspace", wtACleanup)
	}
	if _, err := os.Stat(wtBCleanup); err == nil {
		t.Errorf("expected worktree %s to be removed via linked-worktree workspace", wtBCleanup)
	}
}

func TestE2EWorktreeDeduplicateMixedWorkspace(t *testing.T) {
	parentDir := t.TempDir()

	repoA := filepath.Join(parentDir, "repoA")
	createGitRepo(t, repoA)
	runCmd(t, repoA, "git", "branch", "feat/task")

	wtA := filepath.Join(parentDir, "repoA-task")
	runCmd(t, repoA, "git", "worktree", "add", wtA, "feat/task")

	runCmd(t, repoA, "git", "branch", "feat/cleanup")
	wtACleanup := filepath.Join(parentDir, "repoA-cleanup")
	runCmd(t, repoA, "git", "worktree", "add", wtACleanup, "feat/cleanup")

	cfg := mustConfig(t, defaultExcludeDirs, nil, nil, nil, 20, false, "origin")
	worktreeRemove(context.Background(), parentDir, "feat/cleanup", 2, cfg) //nolint:errcheck

	if _, err := os.Stat(wtACleanup); err == nil {
		t.Errorf("expected worktree %s to be removed", wtACleanup)
	}
	if _, err := os.Stat(wtA); err != nil {
		t.Errorf("expected worktree %s to remain, got: %v", wtA, err)
	}
}

func TestE2EWorktreeOpenIbFilter(t *testing.T) {
	parentDir, repoA, repoB := makeMultiRepoWorkspace(t)

	runCmd(t, repoB, "git", "checkout", "-b", "develop")

	for _, repo := range []string{repoA, repoB} {
		runCmd(t, repo, "git", "branch", "feat/task")
	}

	wtA := filepath.Join(parentDir, "repoA-task")
	wtB := filepath.Join(parentDir, "repoB-task")
	runCmd(t, repoA, "git", "worktree", "add", wtA, "feat/task")
	runCmd(t, repoB, "git", "worktree", "add", wtB, "feat/task")

	oldStdout := os.Stdout
	pr, pw, _ := os.Pipe()
	os.Stdout = pw

	cfg := mustConfig(t, defaultExcludeDirs, nil, nil, []string{"main"}, 20, false, "origin")
	worktreeOpen(context.Background(), parentDir, "feat/task", 2, cfg) //nolint:errcheck

	_ = pw.Close()
	os.Stdout = oldStdout

	outBytes, _ := io.ReadAll(pr)
	output := string(outBytes)

	if !strings.Contains(output, "repoA") {
		t.Errorf("expected repoA in output, got: %s", output)
	}
	if strings.Contains(output, "repoB") {
		t.Errorf("expected repoB to be absent from output (filtered by -ib main), got: %s", output)
	}
}
