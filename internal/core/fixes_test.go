package core

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRunConflictingModes(t *testing.T) {
	err := Run(context.Background(), []string{"-rs", "main", "-rh", "main"})
	if err == nil || !strings.Contains(err.Error(), "conflicting options") {
		t.Fatalf("expected conflicting-options error, got: %v", err)
	}
	err = Run(context.Background(), []string{"-c", "status", "-l"})
	if err == nil || !strings.Contains(err.Error(), "conflicting options") {
		t.Fatalf("expected conflicting-options error, got: %v", err)
	}
}

func TestRunRejectsLeadingDashRefs(t *testing.T) {
	cases := [][]string{
		{"-rb=--exec=echo pwned"},
		{"-rs=--hard"},
		{"-wr=-rf"},
		{"-r=--upload-pack=echo", "-rs", "main"},
	}
	for _, args := range cases {
		if err := Run(context.Background(), args); err == nil || !strings.Contains(err.Error(), "must not start with '-'") {
			t.Errorf("args %v: expected leading-dash rejection, got: %v", args, err)
		}
	}
}

func TestRunDivergeRejectsLeftoverPositional(t *testing.T) {
	err := Run(context.Background(), []string{"-dv", "origin", "main"})
	if err == nil || !strings.Contains(err.Error(), "unexpected argument") {
		t.Fatalf("expected unexpected-argument error, got: %v", err)
	}
}

func TestRunRejectsLeadingDashPositional(t *testing.T) {
	// `--` terminates flag parsing, so a dashed positional would otherwise reach
	// git as an option (e.g. `git switch --detach`).
	err := Run(context.Background(), []string{"--", "--detach"})
	if err == nil || !strings.Contains(err.Error(), "must not start with '-'") {
		t.Fatalf("expected leading-dash rejection for positional, got: %v", err)
	}
}

func TestRunRejectsStrayPositionalForBoolModes(t *testing.T) {
	for _, flag := range []string{"-l", "-tr", "-wl"} {
		err := Run(context.Background(), []string{flag, "main"})
		if err == nil || !strings.Contains(err.Error(), "unexpected argument") {
			t.Errorf("%s: expected unexpected-argument error, got: %v", flag, err)
		}
	}
}

func TestReorderArgsTrackIsBool(t *testing.T) {
	got := reorderArgs([]string{"-tr", "somebranch"})
	want := []string{"-tr", "somebranch"}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("expected %v, got %v", want, got)
	}
}

func TestPreflightScanUnpushedCommits(t *testing.T) {
	repoDir, _ := makeRepoAheadOfOrigin(t)
	repo := RepoInfo{Path: repoDir, RelPath: "repo"}
	plan := classifyReset("origin/main", "", "origin", false)

	dirty := preflightScan(context.Background(), []RepoInfo{repo}, 1, plan, "hard")
	if len(dirty) != 1 || !strings.Contains(dirty[0].DirtyStatus, "1 commit(s) ahead of target") {
		t.Fatalf("expected commits-ahead warning, got: %+v", dirty)
	}

	if dirty := preflightScan(context.Background(), []RepoInfo{repo}, 1, plan, "rebase"); len(dirty) != 0 {
		t.Fatalf("rebase preflight must not warn about commits ahead of target, got: %+v", dirty)
	}
}

func TestPreflightScanStatusFailure(t *testing.T) {
	notARepo := t.TempDir()
	repo := RepoInfo{Path: notARepo, RelPath: "broken"}
	plan := classifyReset("main", "", "origin", false)

	dirty := preflightScan(context.Background(), []RepoInfo{repo}, 1, plan, "hard")
	if len(dirty) != 1 || !strings.Contains(dirty[0].DirtyStatus, "status check failed") {
		t.Fatalf("expected status-check-failed warning, got: %+v", dirty)
	}
}

func TestCheckTrackFailedExitCode(t *testing.T) {
	root := t.TempDir()
	broken := filepath.Join(root, "broken")
	if err := os.MkdirAll(filepath.Join(broken, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}

	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	cfg := mustConfig(t, defaultExcludeDirs, nil, nil, nil, 20, false, "origin")
	err := checkTrack(context.Background(), root, 2, cfg)

	_ = w.Close()
	os.Stdout = oldStdout
	_, _ = io.ReadAll(r)

	if !errors.Is(err, errReposFailed) {
		t.Fatalf("expected errReposFailed for broken repo, got: %v", err)
	}
}

func TestCopyFilePreservesMode(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.env")
	dst := filepath.Join(dir, "dst.env")
	if err := os.WriteFile(src, []byte("SECRET=1"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := copyFile(src, dst); err != nil {
		t.Fatal(err)
	}

	srcInfo, _ := os.Stat(src)
	dstInfo, err := os.Stat(dst)
	if err != nil {
		t.Fatal(err)
	}
	if dstInfo.Mode().Perm() != srcInfo.Mode().Perm() {
		t.Fatalf("expected dst mode %v to match src mode %v", dstInfo.Mode().Perm(), srcInfo.Mode().Perm())
	}
}

func TestPurgeStaleLogDirs(t *testing.T) {
	stale := filepath.Join(os.TempDir(), "gb-logs-stale-test-fixture")
	if err := os.MkdirAll(stale, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(stale) })
	old := time.Now().Add(-8 * 24 * time.Hour)
	if err := os.Chtimes(stale, old, old); err != nil {
		t.Fatal(err)
	}

	lm, err := NewLogManager()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = lm.Cleanup() })

	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatal("expected stale gb-logs dir to be purged")
	}
	if _, err := os.Stat(lm.GetTempDir()); err != nil {
		t.Fatal("current-run log dir must survive the purge")
	}
}
