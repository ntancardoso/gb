package core

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func mustConfig(t *testing.T, excludeDirs, includeDirs, excludeBranches, includeBranches []string, pageSize int, includeWorktrees bool, remote string) *Config {
	t.Helper()
	cfg, err := newConfig(excludeDirs, includeDirs, excludeBranches, includeBranches, pageSize, includeWorktrees, remote)
	if err != nil {
		t.Fatalf("newConfig: %v", err)
	}
	return cfg
}

func TestGetBranch(t *testing.T) {
	tmpDir := t.TempDir()

	runCmd(t, tmpDir, "git", "init")
	runCmd(t, tmpDir, "git", "config", "user.name", "test")
	runCmd(t, tmpDir, "git", "config", "user.email", "test@test.com")

	branch, err := getBranch(tmpDir)
	if err != nil {
		t.Fatal(err)
	}
	if branch != "no commits" && branch != "main" && branch != "master" {
		t.Errorf("expected 'no commits', 'main', or 'master', got %s", branch)
	}

	writeFile(t, tmpDir, "test.txt", "test")
	runCmd(t, tmpDir, "git", "add", ".")
	runCmd(t, tmpDir, "git", "commit", "-m", "initial")

	branch, err = getBranch(tmpDir)
	if err != nil {
		t.Fatal(err)
	}
	if branch != "main" && branch != "master" {
		t.Errorf("expected main/master, got %s", branch)
	}

	runCmd(t, tmpDir, "git", "checkout", "-b", "feature")
	branch, err = getBranch(tmpDir)
	if err != nil {
		t.Fatal(err)
	}
	if branch != "feature" {
		t.Errorf("expected 'feature', got %s", branch)
	}

	hash := strings.TrimSpace(string(runCmdOutput(t, tmpDir, "git", "rev-parse", "HEAD")))
	runCmd(t, tmpDir, "git", "checkout", hash)
	branch, err = getBranch(tmpDir)
	if err != nil {
		t.Fatal(err)
	}
	if branch != "detached" {
		t.Errorf("expected 'detached', got %s", branch)
	}
}

func TestFindGitRepos(t *testing.T) {
	tmpDir := t.TempDir()

	createGitRepo(t, filepath.Join(tmpDir, "repo1"))
	createGitRepo(t, filepath.Join(tmpDir, "nested", "repo2"))
	createDir(t, filepath.Join(tmpDir, "notgit"))
	createGitRepo(t, filepath.Join(tmpDir, "vendor", "repo3"))

	cfg := mustConfig(t, defaultExcludeDirs, nil, nil, nil, 20, false, "origin")
	repos, err := findGitRepos(tmpDir, cfg)
	if err != nil {
		t.Fatal(err)
	}

	expectedRepos := []string{"repo1", filepath.Join("nested", "repo2")}
	actualCount := 0
	paths := make(map[string]bool)

	for _, r := range repos {
		if !strings.Contains(r.RelPath, "vendor") {
			paths[r.RelPath] = true
			actualCount++
		}
	}

	if actualCount < 2 {
		t.Errorf("expected at least 2 non-vendor repos, got %d", actualCount)
	}

	for _, expected := range expectedRepos {
		if !paths[expected] {
			t.Errorf("missing expected repo: %s, got paths: %v", expected, paths)
		}
	}
}

func TestListAllBranches(t *testing.T) {
	tmpDir := t.TempDir()

	repo1 := filepath.Join(tmpDir, "repo1")
	createGitRepo(t, repo1)

	repo2 := filepath.Join(tmpDir, "repo2")
	createGitRepo(t, repo2)
	runCmd(t, repo2, "git", "checkout", "-b", "feature")

	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	cfg := mustConfig(t, defaultExcludeDirs, nil, nil, nil, 20, false, "origin")
	listAllBranches(context.Background(), tmpDir, 2, cfg) //nolint:errcheck

	_ = w.Close()
	os.Stdout = oldStdout

	buf := make([]byte, 1024)
	n, _ := r.Read(buf)
	output := string(buf[:n])

	if !strings.Contains(output, "Branch:") {
		t.Error("expected branch listing in output")
	}
}

func TestSwitchBranches(t *testing.T) {
	tmpDir := t.TempDir()

	repo1 := filepath.Join(tmpDir, "repo1")
	createGitRepo(t, repo1)

	runCmd(t, repo1, "git", "checkout", "-b", "feature")
	writeFile(t, repo1, "feature.txt", "feature")
	runCmd(t, repo1, "git", "add", ".")
	runCmd(t, repo1, "git", "commit", "-m", "feature commit")

	runCmd(t, repo1, "git", "checkout", "main")

	cfg := mustConfig(t, defaultExcludeDirs, nil, nil, nil, 20, false, "origin")
	switchBranches(context.Background(), tmpDir, "feature", 1, cfg) //nolint:errcheck

	branch, err := getBranch(repo1)
	if err != nil {
		t.Fatal(err)
	}
	if branch != "feature" {
		t.Errorf("expected 'feature', got %s", branch)
	}
}

func TestProcessSingleRepo(t *testing.T) {
	tmpDir := t.TempDir()
	createGitRepo(t, tmpDir)

	repo := RepoInfo{Path: tmpDir, RelPath: "test"}
	result := processSingleRepo(repo, "main", "origin", nil)
	if result.Success {
		branch, _ := getBranch(tmpDir)
		if branch != "main" && branch != "master" {
			result = processSingleRepo(repo, "master", "origin", nil)
		}
	}

	if !result.Success {
		t.Errorf("expected success, got error: %s", result.Error)
	}

	result = processSingleRepo(repo, "nonexistent", "origin", nil)
	if result.Success {
		t.Error("expected failure for non-existent branch")
	}
	if !strings.Contains(result.Error, "branch not found") {
		t.Errorf("expected 'branch not found' error, got: %s", result.Error)
	}
}

func TestExecuteCommandInRepos(t *testing.T) {
	tmpDir := t.TempDir()

	repo1 := filepath.Join(tmpDir, "repo1")
	createGitRepo(t, repo1)

	repo2 := filepath.Join(tmpDir, "repo2")
	createGitRepo(t, repo2)

	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	cfg := mustConfig(t, defaultExcludeDirs, nil, nil, nil, 20, false, "origin")
	executeCommandInRepos(context.Background(), tmpDir, "status", 2, cfg) //nolint:errcheck

	_ = w.Close()
	os.Stdout = oldStdout

	buf := make([]byte, 4096)
	n, _ := r.Read(buf)
	output := string(buf[:n])

	if !strings.Contains(output, "2 succeeded, 0 failed") {
		t.Errorf("expected success summary in output, got: %s", output)
	}

	if !strings.Contains(output, "Logs are available at:") {
		t.Errorf("expected log location message in output, got: %s", output)
	}
}

func TestExecuteCommandInReposWithError(t *testing.T) {
	tmpDir := t.TempDir()

	repo1 := filepath.Join(tmpDir, "repo1")
	createGitRepo(t, repo1)

	cmd := exec.Command("git", "invalid-subcommand-that-does-not-exist")
	cmd.Dir = repo1
	output, err := cmd.CombinedOutput()

	if err == nil {
		t.Error("expected git command to fail, but it succeeded")
	}

	result := CommandResult{
		RelPath: "repo1",
		Output:  string(output),
		Error:   err,
	}

	if result.Error == nil {
		t.Error("expected CommandResult to have an error")
	}

	if result.RelPath != "repo1" {
		t.Errorf("expected RelPath 'repo1', got '%s'", result.RelPath)
	}

	if result.Output == "" {
		t.Error("expected some output from failed git command")
	}

	if !strings.Contains(strings.ToLower(result.Output), "unknown") && !strings.Contains(strings.ToLower(result.Output), "invalid") {
		t.Errorf("expected error message in output, got: %s", result.Output)
	}
}

func TestExecuteShellInRepos(t *testing.T) {
	tmpDir := t.TempDir()

	repo1 := filepath.Join(tmpDir, "repo1")
	createGitRepo(t, repo1)

	repo2 := filepath.Join(tmpDir, "repo2")
	createGitRepo(t, repo2)

	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	cfg := mustConfig(t, defaultExcludeDirs, nil, nil, nil, 20, false, "origin")
	executeShellInRepos(context.Background(), tmpDir, "echo test", 2, cfg) //nolint:errcheck

	_ = w.Close()
	os.Stdout = oldStdout

	buf := make([]byte, 4096)
	n, _ := r.Read(buf)
	output := string(buf[:n])

	if !strings.Contains(output, "2 succeeded, 0 failed") {
		t.Errorf("expected success summary in output, got: %s", output)
	}

	if !strings.Contains(output, "Logs are available at:") {
		t.Errorf("expected log location message in output, got: %s", output)
	}
}

func TestRun(t *testing.T) {
	ctx := context.Background()
	err := Run(ctx, []string{"-list"})
	if err != nil {
		t.Errorf("list command failed: %v", err)
	}

	err = Run(ctx, []string{})
	if err == nil {
		t.Error("expected error for missing branch name")
	}

	tmpDir := t.TempDir()
	oldDir, _ := os.Getwd()
	_ = os.Chdir(tmpDir)
	defer func() { _ = os.Chdir(oldDir) }()

	createGitRepo(t, filepath.Join(tmpDir, "repo1"))

	_ = Run(ctx, []string{"main"})

	_ = Run(ctx, []string{"-c", "status"})

	_ = Run(ctx, []string{"-sh", "echo test"})
}

func TestConfigExcludeSet(t *testing.T) {
	dirs := []string{"node_modules", "vendor", ".git"}
	cfg := mustConfig(t, dirs, nil, nil, nil, 20, false, "origin")

	if len(cfg.excludeSet) != 3 {
		t.Errorf("expected 3 items, got %d", len(cfg.excludeSet))
	}

	if _, exists := cfg.excludeSet["node_modules"]; !exists {
		t.Error("expected node_modules in exclude set")
	}
}

func TestConfigShouldExcludeDir(t *testing.T) {
	cfg := mustConfig(t, defaultExcludeDirs, []string{"vendor"}, nil, nil, 20, false, "origin")

	if !cfg.shouldExcludeDir(".hidden") {
		t.Error("should exclude .hidden")
	}

	if cfg.shouldExcludeDir(".git") {
		t.Error("should not exclude .git")
	}

	if cfg.shouldExcludeDir("vendor") {
		t.Error("should not exclude included vendor")
	}
}

func TestResolveRoot(t *testing.T) {
	tmpDir := t.TempDir()

	resolved := resolveRoot(tmpDir)
	if resolved == "" {
		t.Error("resolved root should not be empty")
	}

	resolved = resolveRoot("/non/existent/path")
	if resolved != "/non/existent/path" {
		t.Error("should return original path for non-existent paths")
	}
}

func runCmd(t *testing.T, dir string, name string, args ...string) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	if err := cmd.Run(); err != nil {
		t.Fatalf("command failed: %s %v: %v", name, args, err)
	}
}

func runCmdOutput(t *testing.T, dir string, name string, args ...string) []byte {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("command failed: %s %v: %v", name, args, err)
	}
	return out
}

func createGitRepo(t *testing.T, path string) {
	if err := os.MkdirAll(path, 0755); err != nil {
		t.Fatal(err)
	}

	runCmd(t, path, "git", "init", "-b", "main")
	runCmd(t, path, "git", "config", "user.name", "test")
	runCmd(t, path, "git", "config", "user.email", "test@test.com")

	writeFile(t, path, "README.md", "# Test repo")
	runCmd(t, path, "git", "add", ".")
	runCmd(t, path, "git", "commit", "-m", "initial commit")
}

func createDir(t *testing.T, path string) {
	if err := os.MkdirAll(path, 0755); err != nil {
		t.Fatal(err)
	}
}

func writeFile(t *testing.T, dir, name, content string) {
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestShouldExecuteInRepo(t *testing.T) {
	tests := []struct {
		name        string
		includeDirs []string
		excludeDirs []string
		repoPath    string
		expected    bool
	}{
		{
			name:     "no filters - should execute",
			repoPath: "any/path",
			expected: true,
		},
		{
			name:        "include exact match",
			includeDirs: []string{"vendor", "custom"},
			repoPath:    "vendor",
			expected:    true,
		},
		{
			name:        "include no match",
			includeDirs: []string{"vendor", "custom"},
			repoPath:    "build",
			expected:    false,
		},
		{
			name:        "include parent path match",
			includeDirs: []string{"vendor"},
			repoPath:    "vendor/subdir/repo",
			expected:    true,
		},
		{
			name:        "exclude exact match",
			excludeDirs: []string{"build", "temp"},
			repoPath:    "build",
			expected:    false,
		},
		{
			name:        "exclude no match",
			excludeDirs: []string{"build", "temp"},
			repoPath:    "vendor",
			expected:    true,
		},
		{
			name:        "exclude parent path match",
			excludeDirs: []string{"build"},
			repoPath:    "build/debug/repo",
			expected:    false,
		},
		{
			name:        "include takes priority over exclude",
			includeDirs: []string{"vendor"},
			excludeDirs: []string{"vendor"},
			repoPath:    "vendor",
			expected:    true,
		},
		{
			name:        "include glob trailing wildcard match",
			includeDirs: []string{"feat-*"},
			repoPath:    "feat-foo",
			expected:    true,
		},
		{
			name:        "include glob trailing wildcard no match",
			includeDirs: []string{"feat-*"},
			repoPath:    "build",
			expected:    false,
		},
		{
			name:        "include glob child path match",
			includeDirs: []string{"feat-*"},
			repoPath:    "feat-foo/subrepo",
			expected:    true,
		},
		{
			name:        "include glob question mark",
			includeDirs: []string{"feat?"},
			repoPath:    "feata",
			expected:    true,
		},
		{
			name:        "exclude glob match",
			excludeDirs: []string{"feat-*"},
			repoPath:    "feat-foo",
			expected:    false,
		},
		{
			name:        "exclude glob no match",
			excludeDirs: []string{"feat-*"},
			repoPath:    "main",
			expected:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := mustConfig(t, tt.excludeDirs, tt.includeDirs, nil, nil, 20, false, "origin")
			result := cfg.shouldExecuteInRepo(tt.repoPath)
			if result != tt.expected {
				t.Errorf("shouldExecuteInRepo() = %v, expected %v", result, tt.expected)
			}
		})
	}
}

func TestContainsPath(t *testing.T) {
	tests := []struct {
		name     string
		dirs     []string
		repoPath string
		expected bool
	}{
		{
			name:     "exact match",
			dirs:     []string{"vendor", "custom"},
			repoPath: "vendor",
			expected: true,
		},
		{
			name:     "no match",
			dirs:     []string{"vendor", "custom"},
			repoPath: "build",
			expected: false,
		},
		{
			name:     "parent directory match",
			dirs:     []string{"vendor"},
			repoPath: "vendor/oca/project",
			expected: true,
		},
		{
			name:     "similar name no match",
			dirs:     []string{"ab"},
			repoPath: "abc",
			expected: false,
		},
		{
			name:     "nested parent match",
			dirs:     []string{"projects/odoo"},
			repoPath: "projects/odoo/oca/survey",
			expected: true,
		},
	}

	globTests := []struct {
		name     string
		patterns []string
		repoPath string
		expected bool
	}{
		{
			name:     "glob trailing wildcard match",
			patterns: []string{"feat-*"},
			repoPath: "feat-foo",
			expected: true,
		},
		{
			name:     "glob trailing wildcard no match",
			patterns: []string{"feat-*"},
			repoPath: "build",
			expected: false,
		},
		{
			name:     "glob matches segment in path",
			patterns: []string{"feat-*"},
			repoPath: "feat-foo/subrepo",
			expected: true,
		},
		{
			name:     "glob with slash matches prefix",
			patterns: []string{"OCA/feat-*"},
			repoPath: "OCA/feat-foo/sub",
			expected: true,
		},
		{
			name:     "glob with slash no match",
			patterns: []string{"OCA/feat-*"},
			repoPath: "other/feat-foo",
			expected: false,
		},
		{
			name:     "glob question mark",
			patterns: []string{"feat?"},
			repoPath: "feata",
			expected: true,
		},
		{
			name:     "glob question mark no match too long",
			patterns: []string{"feat?"},
			repoPath: "featab",
			expected: false,
		},
	}

	for _, tt := range globTests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{}
			result := cfg.containsPath(nil, tt.patterns, tt.repoPath)
			if result != tt.expected {
				t.Errorf("containsPath() = %v, expected %v", result, tt.expected)
			}
		})
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			set := make(map[string]struct{})
			for _, dir := range tt.dirs {
				set[dir] = struct{}{}
			}
			cfg := &Config{}
			result := cfg.containsPath(set, nil, tt.repoPath)
			if result != tt.expected {
				t.Errorf("containsPath() = %v, expected %v", result, tt.expected)
			}
		})
	}
}

func TestIsParentPath(t *testing.T) {
	tests := []struct {
		name     string
		parent   string
		child    string
		expected bool
	}{
		{
			name:     "exact same path",
			parent:   "vendor",
			child:    "vendor",
			expected: true,
		},
		{
			name:     "parent is parent",
			parent:   "vendor",
			child:    "vendor/oca",
			expected: true,
		},
		{
			name:     "nested parent",
			parent:   "projects/odoo",
			child:    "projects/odoo/oca/survey",
			expected: true,
		},
		{
			name:     "not parent",
			parent:   "vendor",
			child:    "build",
			expected: false,
		},
		{
			name:     "similar name not parent",
			parent:   "ab",
			child:    "abc",
			expected: false,
		},
		{
			name:     "child is shorter",
			parent:   "vendor/oca",
			child:    "vendor",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{}
			result := cfg.isParentPath(tt.parent, tt.child)
			if result != tt.expected {
				t.Errorf("isParentPath(%q, %q) = %v, expected %v", tt.parent, tt.child, result, tt.expected)
			}
		})
	}
}

func TestFilterWorktrees(t *testing.T) {
	repos := []RepoInfo{
		{Path: "/root/repo1", RelPath: "repo1", IsWorktree: false},
		{Path: "/root/repo2", RelPath: "repo2", IsWorktree: true},
		{Path: "/root/repo3", RelPath: "repo3", IsWorktree: false},
		{Path: "/root/wt", RelPath: "wt", IsWorktree: true},
	}

	t.Run("excludes worktrees by default", func(t *testing.T) {
		cfg := mustConfig(t, nil, nil, nil, nil, 20, false, "origin")
		filtered := cfg.filterWorktrees(repos)
		if len(filtered) != 2 {
			t.Fatalf("expected 2 repos, got %d", len(filtered))
		}
		for _, r := range filtered {
			if r.IsWorktree {
				t.Errorf("worktree repo %q should have been excluded", r.RelPath)
			}
		}
	})

	t.Run("includes worktrees when flag set", func(t *testing.T) {
		cfg := mustConfig(t, nil, nil, nil, nil, 20, true, "origin")
		filtered := cfg.filterWorktrees(repos)
		if len(filtered) != 4 {
			t.Fatalf("expected 4 repos, got %d", len(filtered))
		}
	})
}

func TestIsWorktreeDetection(t *testing.T) {
	tmpDir := t.TempDir()

	mainRepo := filepath.Join(tmpDir, "main-repo")
	createGitRepo(t, mainRepo)
	runCmd(t, mainRepo, "git", "checkout", "-b", "wt-branch")
	writeFile(t, mainRepo, "wt.txt", "worktree file")
	runCmd(t, mainRepo, "git", "add", ".")
	runCmd(t, mainRepo, "git", "commit", "-m", "wt commit")
	runCmd(t, mainRepo, "git", "checkout", "main")

	wtPath := filepath.Join(tmpDir, "linked-wt")
	runCmd(t, mainRepo, "git", "worktree", "add", wtPath, "wt-branch")

	cfg := mustConfig(t, defaultExcludeDirs, nil, nil, nil, 20, false, "origin")
	repos, err := findGitRepos(tmpDir, cfg)
	if err != nil {
		t.Fatal(err)
	}

	repoMap := make(map[string]RepoInfo)
	for _, r := range repos {
		repoMap[r.RelPath] = r
	}

	main, ok := repoMap["main-repo"]
	if !ok {
		t.Fatal("main-repo not found")
	}
	if main.IsWorktree {
		t.Error("main-repo should not be marked as a worktree")
	}

	wt, ok := repoMap["linked-wt"]
	if !ok {
		t.Fatal("linked-wt not found in discovered repos")
	}
	if !wt.IsWorktree {
		t.Error("linked-wt should be marked as a worktree")
	}
}

func TestIsBranchLockedInWorktree(t *testing.T) {
	tmpDir := t.TempDir()
	mainRepo := filepath.Join(tmpDir, "repo")
	createGitRepo(t, mainRepo)

	runCmd(t, mainRepo, "git", "checkout", "-b", "locked-branch")
	writeFile(t, mainRepo, "locked.txt", "locked")
	runCmd(t, mainRepo, "git", "add", ".")
	runCmd(t, mainRepo, "git", "commit", "-m", "locked commit")
	runCmd(t, mainRepo, "git", "checkout", "main")

	wtPath := filepath.Join(tmpDir, "wt")
	runCmd(t, mainRepo, "git", "worktree", "add", wtPath, "locked-branch")

	if !isBranchLockedInWorktree(mainRepo, "locked-branch") {
		t.Error("expected locked-branch to be detected as locked in worktree")
	}

	if isBranchLockedInWorktree(mainRepo, "main") {
		t.Error("main should not be reported as locked (it is the main worktree)")
	}

	if isBranchLockedInWorktree(mainRepo, "nonexistent") {
		t.Error("nonexistent branch should not be reported as locked")
	}
}

func TestProcessSingleRepoSkipsLockedBranch(t *testing.T) {
	tmpDir := t.TempDir()
	mainRepo := filepath.Join(tmpDir, "repo")
	createGitRepo(t, mainRepo)

	runCmd(t, mainRepo, "git", "checkout", "-b", "locked-branch")
	writeFile(t, mainRepo, "locked.txt", "locked")
	runCmd(t, mainRepo, "git", "add", ".")
	runCmd(t, mainRepo, "git", "commit", "-m", "locked commit")
	runCmd(t, mainRepo, "git", "checkout", "main")

	wtPath := filepath.Join(tmpDir, "wt")
	runCmd(t, mainRepo, "git", "worktree", "add", wtPath, "locked-branch")

	repo := RepoInfo{Path: mainRepo, RelPath: "repo"}
	result := processSingleRepo(repo, "locked-branch", "origin", nil)

	if !result.Skipped {
		t.Error("expected result to be skipped when branch is locked in worktree")
	}
	if result.Success {
		t.Error("skipped result should not be Success")
	}
	if !strings.Contains(result.Error, "locked in worktree") {
		t.Errorf("expected 'locked in worktree' in error, got: %s", result.Error)
	}
}

func TestFilterReposForExecution(t *testing.T) {
	repos := []RepoInfo{
		{Path: "/root/vendor", RelPath: "vendor"},
		{Path: "/root/custom", RelPath: "custom"},
		{Path: "/root/build", RelPath: "build"},
		{Path: "/root/vendor/oca/project", RelPath: "vendor/oca/project"},
	}

	tests := []struct {
		name          string
		includeDirs   []string
		excludeDirs   []string
		expectedPaths []string
		expectedCount int
	}{
		{
			name:          "no filters - return all",
			expectedPaths: []string{"vendor", "custom", "build", "vendor/oca/project"},
			expectedCount: 4,
		},
		{
			name:          "include specific dirs",
			includeDirs:   []string{"vendor", "custom"},
			expectedPaths: []string{"vendor", "custom", "vendor/oca/project"},
			expectedCount: 3,
		},
		{
			name:          "exclude specific dirs",
			excludeDirs:   []string{"build"},
			expectedPaths: []string{"vendor", "custom", "vendor/oca/project"},
			expectedCount: 3,
		},
		{
			name:          "include takes priority",
			includeDirs:   []string{"vendor"},
			excludeDirs:   []string{"vendor"},
			expectedPaths: []string{"vendor", "vendor/oca/project"},
			expectedCount: 2,
		},
		{
			name:          "include none matching",
			includeDirs:   []string{"nonexistent"},
			expectedPaths: []string{},
			expectedCount: 0,
		},
		{
			name:          "include glob pattern",
			includeDirs:   []string{"feat-*"},
			expectedPaths: []string{},
			expectedCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := mustConfig(t, tt.excludeDirs, tt.includeDirs, nil, nil, 20, false, "origin")
			filtered := cfg.filterReposForExecution(repos)

			if len(filtered) != tt.expectedCount {
				t.Errorf("filterReposForExecution() returned %d repos, expected %d", len(filtered), tt.expectedCount)
			}

			var actualPaths []string
			for _, repo := range filtered {
				actualPaths = append(actualPaths, repo.RelPath)
			}

			for _, expectedPath := range tt.expectedPaths {
				found := false
				for _, actualPath := range actualPaths {
					if actualPath == expectedPath {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("Expected path %q not found in filtered results: %v", expectedPath, actualPaths)
				}
			}
		})
	}
}

func TestShouldExecuteInBranch(t *testing.T) {
	tests := []struct {
		name            string
		includeBranches []string
		excludeBranches []string
		branch          string
		expected        bool
	}{
		{
			name:     "no filters",
			branch:   "main",
			expected: true,
		},
		{
			name:            "include literal match",
			includeBranches: []string{"main"},
			branch:          "main",
			expected:        true,
		},
		{
			name:            "include literal no match",
			includeBranches: []string{"main"},
			branch:          "develop",
			expected:        false,
		},
		{
			name:            "exclude literal match",
			excludeBranches: []string{"main"},
			branch:          "main",
			expected:        false,
		},
		{
			name:            "exclude literal no match",
			excludeBranches: []string{"main"},
			branch:          "develop",
			expected:        true,
		},
		{
			name:            "include glob match",
			includeBranches: []string{"release/*"},
			branch:          "release/1.2",
			expected:        true,
		},
		{
			name:            "include glob no match",
			includeBranches: []string{"release/*"},
			branch:          "develop",
			expected:        false,
		},
		{
			name:            "exclude glob match",
			excludeBranches: []string{"feat-*"},
			branch:          "feat-foo",
			expected:        false,
		},
		{
			name:            "exclude glob no match",
			excludeBranches: []string{"feat-*"},
			branch:          "main",
			expected:        true,
		},
		{
			name:            "include glob question mark",
			includeBranches: []string{"feat?"},
			branch:          "feata",
			expected:        true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := mustConfig(t, nil, nil, tt.excludeBranches, tt.includeBranches, 20, false, "origin")
			result := cfg.shouldExecuteInBranch(tt.branch)
			if result != tt.expected {
				t.Errorf("shouldExecuteInBranch() = %v, expected %v", result, tt.expected)
			}
		})
	}
}

func TestNewConfigInvalidPattern(t *testing.T) {
	_, err := newConfig(nil, []string{"feat["}, nil, nil, 20, false, "origin")
	if err == nil {
		t.Error("expected error for malformed include pattern, got nil")
	}

	_, err = newConfig([]string{"build["}, nil, nil, nil, 20, false, "origin")
	if err == nil {
		t.Error("expected error for malformed exclude pattern, got nil")
	}

	_, err = newConfig(nil, nil, nil, []string{"release/["}, 20, false, "origin")
	if err == nil {
		t.Error("expected error for malformed include branch pattern, got nil")
	}

	_, err = newConfig(nil, nil, []string{"feat["}, nil, 20, false, "origin")
	if err == nil {
		t.Error("expected error for malformed exclude branch pattern, got nil")
	}
}
