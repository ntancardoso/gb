package core

import "testing"

func TestProcessSingleDivergeUpToDate(t *testing.T) {
	repoDir, _ := makeRepoWithRemote(t)
	repo := RepoInfo{Path: repoDir, RelPath: "repo"}
	res := processSingleDiverge(repo, "main", "origin")

	if !res.Success {
		t.Fatalf("expected success, got error: %s", res.Error)
	}
	if res.Skipped {
		t.Fatalf("expected not skipped, got: %s", res.SkipReason)
	}
	if res.Ahead != 0 || res.Behind != 0 {
		t.Errorf("expected 0 ahead 0 behind, got ahead=%d behind=%d", res.Ahead, res.Behind)
	}
}

func TestProcessSingleDivergeBehind(t *testing.T) {
	repoDir, remoteDir := makeRepoWithRemote(t)

	otherDir := t.TempDir()
	runCmd(t, otherDir, "git", "init", "-b", "main")
	runCmd(t, otherDir, "git", "config", "user.name", "test")
	runCmd(t, otherDir, "git", "config", "user.email", "test@test.com")
	runCmd(t, otherDir, "git", "remote", "add", "origin", remoteDir)
	runCmd(t, otherDir, "git", "fetch", "origin")
	runCmd(t, otherDir, "git", "checkout", "-b", "main", "--track", "origin/main")
	writeFile(t, otherDir, "remote-only.txt", "remote commit")
	runCmd(t, otherDir, "git", "add", ".")
	runCmd(t, otherDir, "git", "commit", "-m", "remote commit")
	runCmd(t, otherDir, "git", "push", "origin", "main")

	runCmd(t, repoDir, "git", "fetch", "origin")

	repo := RepoInfo{Path: repoDir, RelPath: "repo"}
	res := processSingleDiverge(repo, "main", "origin")

	if !res.Success {
		t.Fatalf("expected success, got error: %s", res.Error)
	}
	if res.Ahead != 0 || res.Behind != 1 {
		t.Errorf("expected 0 ahead 1 behind, got ahead=%d behind=%d", res.Ahead, res.Behind)
	}
}

func TestProcessSingleDivergeAhead(t *testing.T) {
	repoDir, _ := makeRepoWithRemote(t)

	writeFile(t, repoDir, "local-only.txt", "local commit")
	runCmd(t, repoDir, "git", "add", ".")
	runCmd(t, repoDir, "git", "commit", "-m", "local commit")

	repo := RepoInfo{Path: repoDir, RelPath: "repo"}
	res := processSingleDiverge(repo, "main", "origin")

	if !res.Success {
		t.Fatalf("expected success, got error: %s", res.Error)
	}
	if res.Ahead != 1 || res.Behind != 0 {
		t.Errorf("expected 1 ahead 0 behind, got ahead=%d behind=%d", res.Ahead, res.Behind)
	}
}

func TestProcessSingleDivergeDiverged(t *testing.T) {
	repoDir, remoteDir := makeRepoWithRemote(t)

	otherDir := t.TempDir()
	runCmd(t, otherDir, "git", "init", "-b", "main")
	runCmd(t, otherDir, "git", "config", "user.name", "test")
	runCmd(t, otherDir, "git", "config", "user.email", "test@test.com")
	runCmd(t, otherDir, "git", "remote", "add", "origin", remoteDir)
	runCmd(t, otherDir, "git", "fetch", "origin")
	runCmd(t, otherDir, "git", "checkout", "-b", "main", "--track", "origin/main")
	writeFile(t, otherDir, "remote-only.txt", "remote commit")
	runCmd(t, otherDir, "git", "add", ".")
	runCmd(t, otherDir, "git", "commit", "-m", "remote commit")
	runCmd(t, otherDir, "git", "push", "origin", "main")

	runCmd(t, repoDir, "git", "fetch", "origin")
	writeFile(t, repoDir, "local-only.txt", "local commit")
	runCmd(t, repoDir, "git", "add", ".")
	runCmd(t, repoDir, "git", "commit", "-m", "local commit")

	repo := RepoInfo{Path: repoDir, RelPath: "repo"}
	res := processSingleDiverge(repo, "main", "origin")

	if !res.Success {
		t.Fatalf("expected success, got error: %s", res.Error)
	}
	if res.Ahead != 1 || res.Behind != 1 {
		t.Errorf("expected 1 ahead 1 behind, got ahead=%d behind=%d", res.Ahead, res.Behind)
	}
}

func TestProcessSingleDivergeTrackingBranch(t *testing.T) {
	repoDir, _ := makeRepoWithRemote(t)
	runCmd(t, repoDir, "git", "branch", "--set-upstream-to=origin/main", "main")

	repo := RepoInfo{Path: repoDir, RelPath: "repo"}
	res := processSingleDiverge(repo, "", "origin")

	if !res.Success {
		t.Fatalf("expected success, got error=%q skipped=%v reason=%q", res.Error, res.Skipped, res.SkipReason)
	}
	if res.UpstreamRef != "origin/main" {
		t.Errorf("expected UpstreamRef=origin/main, got %q", res.UpstreamRef)
	}
	if res.Ahead != 0 || res.Behind != 0 {
		t.Errorf("expected 0/0, got ahead=%d behind=%d", res.Ahead, res.Behind)
	}
}

func TestProcessSingleDivergeNoTracking(t *testing.T) {
	repoDir, _ := makeRepoWithRemote(t)

	repo := RepoInfo{Path: repoDir, RelPath: "repo"}
	res := processSingleDiverge(repo, "", "origin")

	if res.Error != "" {
		t.Fatalf("unexpected error: %s", res.Error)
	}
	if !res.Skipped || res.SkipReason != "no upstream tracking" {
		t.Errorf("expected skipped with 'no upstream tracking', got skipped=%v reason=%q", res.Skipped, res.SkipReason)
	}
}

func TestProcessSingleDivergeRemoteRefNotFound(t *testing.T) {
	repoDir := t.TempDir()
	createGitRepo(t, repoDir)

	repo := RepoInfo{Path: repoDir, RelPath: "repo"}
	res := processSingleDiverge(repo, "main", "origin")

	if res.Error != "" {
		t.Fatalf("expected no error, got: %s", res.Error)
	}
	if !res.Skipped {
		t.Fatalf("expected skipped, got success with ahead=%d behind=%d", res.Ahead, res.Behind)
	}
	if res.SkipReason != "remote ref not found" {
		t.Errorf("expected 'remote ref not found', got %q", res.SkipReason)
	}
}
