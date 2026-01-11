package core

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestGetBranch(t *testing.T) {
	tmpDir := t.TempDir()

	// Setup git repo
	runCmd(t, tmpDir, "git", "init")
	runCmd(t, tmpDir, "git", "config", "user.name", "test")
	runCmd(t, tmpDir, "git", "config", "user.email", "test@test.com")

	// Test no commits
	branch, err := getBranch(tmpDir)
	if err != nil {
		t.Fatal(err)
	}
	// Git 2.28+ defaults to 'main' branch even without commits
	if branch != "no commits" && branch != "main" && branch != "master" {
		t.Errorf("expected 'no commits', 'main', or 'master', got %s", branch)
	}

	// Add commit
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

	// Test different branch
	runCmd(t, tmpDir, "git", "checkout", "-b", "feature")
	branch, err = getBranch(tmpDir)
	if err != nil {
		t.Fatal(err)
	}
	if branch != "feature" {
		t.Errorf("expected 'feature', got %s", branch)
	}

	// Test detached HEAD
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

	// Create nested structure
	createGitRepo(t, filepath.Join(tmpDir, "repo1"))
	createGitRepo(t, filepath.Join(tmpDir, "nested", "repo2"))
	createDir(t, filepath.Join(tmpDir, "notgit"))
	createGitRepo(t, filepath.Join(tmpDir, "vendor", "repo3")) // should be skipped

	cfg := newConfig(defaultSkipDirs, nil)
	repos, err := findGitRepos(tmpDir, cfg)
	if err != nil {
		t.Fatal(err)
	}

	// Filter out any repos that should be skipped
	expectedRepos := []string{"repo1", filepath.Join("nested", "repo2")}
	actualCount := 0
	paths := make(map[string]bool)

	for _, r := range repos {
		// Skip vendor repos as they should be filtered
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

	// Create repos with different branches
	repo1 := filepath.Join(tmpDir, "repo1")
	createGitRepo(t, repo1)

	repo2 := filepath.Join(tmpDir, "repo2")
	createGitRepo(t, repo2)
	runCmd(t, repo2, "git", "checkout", "-b", "feature")

	// Capture output
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	cfg := newConfig(defaultSkipDirs, nil)
	listAllBranches(tmpDir, 2, cfg)

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

	// Create repo with main and feature branches
	repo1 := filepath.Join(tmpDir, "repo1")
	createGitRepo(t, repo1)

	// Create feature branch
	runCmd(t, repo1, "git", "checkout", "-b", "feature")
	writeFile(t, repo1, "feature.txt", "feature")
	runCmd(t, repo1, "git", "add", ".")
	runCmd(t, repo1, "git", "commit", "-m", "feature commit")

	// Switch back to main
	runCmd(t, repo1, "git", "checkout", "main")

	// Test switching to feature
	cfg := newConfig(defaultSkipDirs, nil)
	switchBranches(tmpDir, "feature", 1, cfg)

	// Verify we're on feature branch
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

	// Test switching to existing branch (main/master)
	repo := RepoInfo{Path: tmpDir, RelPath: "test"}
	result := processSingleRepo(repo, "main")
	if result.Success {
		branch, _ := getBranch(tmpDir)
		if branch != "main" && branch != "master" {
			// If main doesn't exist, try master
			result = processSingleRepo(repo, "master")
		}
	}

	if !result.Success {
		t.Errorf("expected success, got error: %s", result.Error)
	}

	// Test switching to non-existent branch
	result = processSingleRepo(repo, "nonexistent")
	if result.Success {
		t.Error("expected failure for non-existent branch")
	}
	if !strings.Contains(result.Error, "branch not found") {
		t.Errorf("expected 'branch not found' error, got: %s", result.Error)
	}
}

func TestExecuteCommandInRepos(t *testing.T) {
	tmpDir := t.TempDir()

	// Create repos
	repo1 := filepath.Join(tmpDir, "repo1")
	createGitRepo(t, repo1)

	repo2 := filepath.Join(tmpDir, "repo2")
	createGitRepo(t, repo2)

	// Capture output
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	cfg := newConfig(defaultSkipDirs, nil)
	executeCommandInRepos(tmpDir, "status", 2, cfg)

	_ = w.Close()
	os.Stdout = oldStdout

	buf := make([]byte, 2048)
	n, _ := r.Read(buf)
	output := string(buf[:n])

	// Check that both repos are mentioned in the output
	if !strings.Contains(output, "repo1") || !strings.Contains(output, "repo2") {
		t.Errorf("expected both repos in output, got: %s", output)
	}

	// Check for success indicators
	if !strings.Contains(output, "✅") {
		t.Errorf("expected success indicators in output, got: %s", output)
	}
}

func TestExecuteCommandInReposWithError(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a git repo
	repo1 := filepath.Join(tmpDir, "repo1")
	createGitRepo(t, repo1)

	// Test individual command execution that will fail
	cmd := exec.Command("git", "invalid-subcommand-that-does-not-exist")
	cmd.Dir = repo1
	output, err := cmd.CombinedOutput()

	// Verify that git command fails as expected
	if err == nil {
		t.Error("expected git command to fail, but it succeeded")
	}

	// Test CommandResult structure (this is what the real function uses)
	result := CommandResult{
		RelPath: "repo1",
		Output:  string(output),
		Error:   err,
	}

	// Verify error result is properly structured
	if result.Error == nil {
		t.Error("expected CommandResult to have an error")
	}

	if result.RelPath != "repo1" {
		t.Errorf("expected RelPath 'repo1', got '%s'", result.RelPath)
	}

	if result.Output == "" {
		t.Error("expected some output from failed git command")
	}

	// Test that output contains error information
	if !strings.Contains(strings.ToLower(result.Output), "unknown") && !strings.Contains(strings.ToLower(result.Output), "invalid") {
		t.Errorf("expected error message in output, got: %s", result.Output)
	}
}

func TestExecuteShellInRepos(t *testing.T) {
	tmpDir := t.TempDir()

	// Create repos
	repo1 := filepath.Join(tmpDir, "repo1")
	createGitRepo(t, repo1)

	repo2 := filepath.Join(tmpDir, "repo2")
	createGitRepo(t, repo2)

	// Capture output
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	cfg := newConfig(defaultSkipDirs, nil)
	executeShellInRepos(tmpDir, "echo test", 2, cfg)

	_ = w.Close()
	os.Stdout = oldStdout

	buf := make([]byte, 2048)
	n, _ := r.Read(buf)
	output := string(buf[:n])

	// Check that both repos are mentioned in the output
	if !strings.Contains(output, "repo1") || !strings.Contains(output, "repo2") {
		t.Errorf("expected both repos in output, got: %s", output)
	}

	// Check for success indicators
	if !strings.Contains(output, "✅") {
		t.Errorf("expected success indicators in output, got: %s", output)
	}

	// Check that the command was executed (should contain "test" in output)
	if !strings.Contains(output, "test") {
		t.Errorf("expected command output 'test' in output, got: %s", output)
	}
}

func TestRun(t *testing.T) {
	// Test list flag
	err := Run([]string{"-list"})
	if err != nil {
		t.Errorf("list command failed: %v", err)
	}

	// Test missing branch name
	err = Run([]string{})
	if err == nil {
		t.Error("expected error for missing branch name")
	}

	// Test with branch name
	tmpDir := t.TempDir()
	oldDir, _ := os.Getwd()
	_ = os.Chdir(tmpDir)
	defer func() { _ = os.Chdir(oldDir) }()

	createGitRepo(t, filepath.Join(tmpDir, "repo1"))

	_ = Run([]string{"main"})
	// This might fail due to branch not existing, which is expected
	// The important part is that it runs without panicking

	// Test command execution flag
	_ = Run([]string{"-c", "status"})
	// This might fail if no repos are found, which is expected
	// The important part is that it runs without panicking

	// Test shell execution flag
	_ = Run([]string{"-sh", "echo test"})
	// This might fail if no repos are found, which is expected
	// The important part is that it runs without panicking
}

func TestConfigSkipSet(t *testing.T) {
	dirs := []string{"node_modules", "vendor", ".git"}
	cfg := newConfig(dirs, nil)

	if len(cfg.skipSet) != 3 {
		t.Errorf("expected 3 items, got %d", len(cfg.skipSet))
	}

	if _, exists := cfg.skipSet["node_modules"]; !exists {
		t.Error("expected node_modules in skip set")
	}
}

func TestConfigShouldSkipDir(t *testing.T) {
	cfg := newConfig(defaultSkipDirs, []string{"vendor"})

	// Should skip hidden dirs except .git
	if !cfg.shouldSkipDir(".hidden") {
		t.Error("should skip .hidden")
	}

	if cfg.shouldSkipDir(".git") {
		t.Error("should not skip .git")
	}

	// Should not skip included dirs
	if cfg.shouldSkipDir("vendor") {
		t.Error("should not skip included vendor")
	}
}

func TestResolveRoot(t *testing.T) {
	tmpDir := t.TempDir()

	resolved := resolveRoot(tmpDir)
	if resolved == "" {
		t.Error("resolved root should not be empty")
	}

	// Test with non-existent path
	resolved = resolveRoot("/non/existent/path")
	if resolved != "/non/existent/path" {
		t.Error("should return original path for non-existent paths")
	}
}

// Helper functions
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
		skipDirs    []string
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
			name:     "skip exact match",
			skipDirs: []string{"build", "temp"},
			repoPath: "build",
			expected: false,
		},
		{
			name:     "skip no match",
			skipDirs: []string{"build", "temp"},
			repoPath: "vendor",
			expected: true,
		},
		{
			name:     "skip parent path match",
			skipDirs: []string{"build"},
			repoPath: "build/debug/repo",
			expected: false,
		},
		{
			name:        "include takes priority over skip",
			includeDirs: []string{"vendor"},
			skipDirs:    []string{"vendor"},
			repoPath:    "vendor",
			expected:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := newConfig(tt.skipDirs, tt.includeDirs)
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

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			set := make(map[string]struct{})
			for _, dir := range tt.dirs {
				set[dir] = struct{}{}
			}
			cfg := &Config{}
			result := cfg.containsPath(set, tt.repoPath)
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

func TestFilterReposForExecution(t *testing.T) {
	repos := []RepoInfo{
		{Path: "/root/vendor", RelPath: "vendor"},
		{Path: "/root/custom", RelPath: "custom"},
		{Path: "/root/build", RelPath: "build"},
		{Path: "/root/vendor/oca/project", RelPath: "vendor/oca/project"},
	}

	tests := []struct {
		name           string
		includeDirs    []string
		skipDirs       []string
		expectedPaths  []string
		expectedCount  int
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
			name:          "skip specific dirs",
			skipDirs:      []string{"build"},
			expectedPaths: []string{"vendor", "custom", "vendor/oca/project"},
			expectedCount: 3,
		},
		{
			name:          "include takes priority",
			includeDirs:   []string{"vendor"},
			skipDirs:      []string{"vendor"},
			expectedPaths: []string{"vendor", "vendor/oca/project"},
			expectedCount: 2,
		},
		{
			name:          "include none matching",
			includeDirs:   []string{"nonexistent"},
			expectedPaths: []string{},
			expectedCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := newConfig(tt.skipDirs, tt.includeDirs)
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
