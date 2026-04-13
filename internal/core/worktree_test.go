package core

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

func TestParseWorktreeList(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  [][2]string
	}{
		{
			name:  "single main worktree",
			input: "worktree /projects/api\nHEAD abc1234\nbranch refs/heads/main\n\n",
			want:  [][2]string{{"main", "/projects/api"}},
		},
		{
			name:  "main plus linked worktree",
			input: "worktree /projects/api\nHEAD abc1234\nbranch refs/heads/main\n\nworktree /projects/api-login\nHEAD def5678\nbranch refs/heads/feat/login\n\n",
			want: [][2]string{
				{"main", "/projects/api"},
				{"feat/login", "/projects/api-login"},
			},
		},
		{
			name:  "bare worktree skipped",
			input: "worktree /projects/api\nHEAD abc1234\nbranch refs/heads/main\n\nworktree /projects/api-bare\nHEAD abc1234\nbare\n\n",
			want:  [][2]string{{"main", "/projects/api"}},
		},
		{
			name:  "detached HEAD skipped",
			input: "worktree /projects/api\nHEAD abc1234\nbranch refs/heads/main\n\nworktree /projects/api-detached\nHEAD def5678\ndetached\n\n",
			want:  [][2]string{{"main", "/projects/api"}},
		},
		{
			name:  "empty output",
			input: "",
			want:  nil,
		},
		{
			name:  "no trailing newline",
			input: "worktree /projects/api\nHEAD abc1234\nbranch refs/heads/main",
			want:  [][2]string{{"main", "/projects/api"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseWorktreeList(tt.input)
			if len(got) != len(tt.want) {
				t.Fatalf("parseWorktreeList() len = %d, want %d; got %v", len(got), len(tt.want), got)
			}
			for i, pair := range got {
				if pair != tt.want[i] {
					t.Errorf("pair[%d] = %v, want %v", i, pair, tt.want[i])
				}
			}
		})
	}
}

func TestFilterReposByBranchGlobPattern(t *testing.T) {
	tmpDir := t.TempDir()

	repo1 := filepath.Join(tmpDir, "repo1")
	repo2 := filepath.Join(tmpDir, "repo2")
	createGitRepo(t, repo1)
	createGitRepo(t, repo2)

	runCmd(t, repo1, "git", "checkout", "-b", "feat/AB-123")
	runCmd(t, repo2, "git", "checkout", "-b", "fix/some-bug")

	repos := []RepoInfo{
		{Path: repo1, RelPath: "repo1"},
		{Path: repo2, RelPath: "repo2"},
	}

	cfg := mustConfig(t, nil, nil, nil, []string{"feat/*"}, 20, false, "origin")
	filtered := cfg.filterReposByBranch(repos, 2)

	if len(filtered) != 1 {
		t.Fatalf("expected 1 repo, got %d: %v", len(filtered), filtered)
	}
	if filtered[0].RelPath != "repo1" {
		t.Errorf("expected repo1, got %s", filtered[0].RelPath)
	}
}

func TestFilterReposByBranchGlobExclude(t *testing.T) {
	tmpDir := t.TempDir()

	repo1 := filepath.Join(tmpDir, "repo1")
	repo2 := filepath.Join(tmpDir, "repo2")
	createGitRepo(t, repo1)
	createGitRepo(t, repo2)

	runCmd(t, repo1, "git", "checkout", "-b", "feat/AB-123")

	repos := []RepoInfo{
		{Path: repo1, RelPath: "repo1"},
		{Path: repo2, RelPath: "repo2"},
	}

	cfg := mustConfig(t, nil, nil, []string{"feat/*"}, nil, 20, false, "origin")
	filtered := cfg.filterReposByBranch(repos, 2)

	if len(filtered) != 1 {
		t.Fatalf("expected 1 repo, got %d: %v", len(filtered), filtered)
	}
	if filtered[0].RelPath != "repo2" {
		t.Errorf("expected repo2, got %s", filtered[0].RelPath)
	}
}

func TestWorktreeRemoveGlob(t *testing.T) {
	tmpDir := t.TempDir()
	repoDir := filepath.Join(tmpDir, "myrepo")
	createGitRepo(t, repoDir)

	runCmd(t, repoDir, "git", "branch", "feat/AB-100")
	runCmd(t, repoDir, "git", "branch", "feat/AB-200")
	runCmd(t, repoDir, "git", "branch", "fix/bug-1")

	wt1 := filepath.Join(tmpDir, "myrepo-AB-100")
	wt2 := filepath.Join(tmpDir, "myrepo-AB-200")
	wtFix := filepath.Join(tmpDir, "myrepo-bug-1")

	runCmd(t, repoDir, "git", "worktree", "add", wt1, "feat/AB-100")
	runCmd(t, repoDir, "git", "worktree", "add", wt2, "feat/AB-200")
	runCmd(t, repoDir, "git", "worktree", "add", wtFix, "fix/bug-1")

	cfg := mustConfig(t, defaultExcludeDirs, nil, nil, nil, 20, false, "origin")
	worktreeRemove(context.Background(), tmpDir, "feat/*", 2, cfg) //nolint:errcheck

	if _, err := os.Stat(wt1); err == nil {
		t.Errorf("expected worktree %s to be removed", wt1)
	}
	if _, err := os.Stat(wt2); err == nil {
		t.Errorf("expected worktree %s to be removed", wt2)
	}
	if _, err := os.Stat(wtFix); err != nil {
		t.Errorf("expected worktree %s to remain, got: %v", wtFix, err)
	}
}

func TestWorktreeRemoveExact(t *testing.T) {
	tmpDir := t.TempDir()
	repoDir := filepath.Join(tmpDir, "myrepo")
	createGitRepo(t, repoDir)

	runCmd(t, repoDir, "git", "branch", "feat/task-1")
	runCmd(t, repoDir, "git", "branch", "feat/task-2")

	wt1 := filepath.Join(tmpDir, "myrepo-task-1")
	wt2 := filepath.Join(tmpDir, "myrepo-task-2")

	runCmd(t, repoDir, "git", "worktree", "add", wt1, "feat/task-1")
	runCmd(t, repoDir, "git", "worktree", "add", wt2, "feat/task-2")

	cfg := mustConfig(t, defaultExcludeDirs, nil, nil, nil, 20, false, "origin")
	worktreeRemove(context.Background(), tmpDir, "feat/task-1", 2, cfg) //nolint:errcheck

	if _, err := os.Stat(wt1); err == nil {
		t.Errorf("expected worktree %s to be removed", wt1)
	}
	if _, err := os.Stat(wt2); err != nil {
		t.Errorf("expected worktree %s to remain, got: %v", wt2, err)
	}
}

func TestWorktreeListAll(t *testing.T) {
	tmpDir := t.TempDir()
	repoDir := filepath.Join(tmpDir, "myrepo")
	createGitRepo(t, repoDir)

	runCmd(t, repoDir, "git", "branch", "feat/task-1")
	wtPath := filepath.Join(tmpDir, "myrepo-task-1")
	runCmd(t, repoDir, "git", "worktree", "add", wtPath, "feat/task-1")

	cfg := mustConfig(t, defaultExcludeDirs, nil, nil, nil, 20, false, "origin")
	worktreeListAll(context.Background(), tmpDir, 2, cfg) //nolint:errcheck
}

func TestWorktreePath(t *testing.T) {
	tests := []struct {
		repoPath string
		branch   string
		want     string
	}{
		{
			repoPath: filepath.Join("projects", "api"),
			branch:   "feat/login",
			want:     filepath.Join("projects", "api-login"),
		},
		{
			repoPath: filepath.Join("projects", "api"),
			branch:   "main",
			want:     filepath.Join("projects", "api-main"),
		},
		{
			repoPath: filepath.Join("projects", "web"),
			branch:   "release/1.2.3",
			want:     filepath.Join("projects", "web-1.2.3"),
		},
	}

	for _, tt := range tests {
		got := worktreePath(tt.repoPath, tt.branch)
		if got != tt.want {
			t.Errorf("worktreePath(%q, %q) = %q, want %q", tt.repoPath, tt.branch, got, tt.want)
		}
	}
}

func TestWorktreeOpenWithBranchFilter(t *testing.T) {
	tmpDir := t.TempDir()

	repo1 := filepath.Join(tmpDir, "repo1")
	repo2 := filepath.Join(tmpDir, "repo2")
	createGitRepo(t, repo1)
	createGitRepo(t, repo2)

	runCmd(t, repo1, "git", "checkout", "-b", "feat/task")

	cfg := mustConfig(t, defaultExcludeDirs, nil, nil, []string{"feat/*"}, 20, false, "origin")
	worktreeOpen(context.Background(), tmpDir, "feat/task", 2, cfg) //nolint:errcheck
}

func TestParseWorktreeListOrdering(t *testing.T) {
	input := "worktree /a\nHEAD 111\nbranch refs/heads/main\n\nworktree /b\nHEAD 222\nbranch refs/heads/feat/x\n\nworktree /c\nHEAD 333\nbranch refs/heads/feat/y\n\n"
	got := parseWorktreeList(input)

	if len(got) != 3 {
		t.Fatalf("expected 3 pairs, got %d", len(got))
	}

	branches := make([]string, len(got))
	for i, p := range got {
		branches[i] = p[0]
	}
	sort.Strings(branches)
	expected := []string{"feat/x", "feat/y", "main"}
	for i, b := range branches {
		if b != expected[i] {
			t.Errorf("branches[%d] = %q, want %q", i, b, expected[i])
		}
	}
}
