package core

import "testing"

func TestProcessSingleTrackWithUpstream(t *testing.T) {
	remoteDir := t.TempDir()
	repoDir := t.TempDir()

	runCmd(t, remoteDir, "git", "init", "--bare", "-b", "main")
	createGitRepo(t, repoDir)
	runCmd(t, repoDir, "git", "remote", "add", "origin", remoteDir)
	runCmd(t, repoDir, "git", "push", "-u", "origin", "main")

	repo := RepoInfo{Path: repoDir, RelPath: "repo"}
	res := processSingleTrack(repo)

	if res.Error != "" {
		t.Fatalf("expected no error, got: %s", res.Error)
	}
	if res.Upstream == "(none)" {
		t.Fatal("expected upstream to be set, got (none)")
	}
	if res.Upstream != "origin/main" {
		t.Errorf("expected 'origin/main', got %q", res.Upstream)
	}
}

func TestProcessSingleTrackNoUpstream(t *testing.T) {
	repoDir := t.TempDir()
	createGitRepo(t, repoDir)

	repo := RepoInfo{Path: repoDir, RelPath: "repo"}
	res := processSingleTrack(repo)

	if res.Error != "" {
		t.Fatalf("expected no error, got: %s", res.Error)
	}
	if res.Upstream != "(none)" {
		t.Errorf("expected '(none)', got %q", res.Upstream)
	}
}
