package core

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// RepoInfo contains information about a discovered git repository.
type RepoInfo struct {
	Path    string // Absolute path to the repository
	RelPath string // Path relative to the scan root
}

// resolveRoot normalizes root path for Windows junctions and WSL symlinks.
// Returns the resolved path and a boolean indicating if resolution occurred.
func resolveRoot(root string) string {
	// First try EvalSymlinks (handles junctions/symlinks generally)
	if realRoot, err := filepath.EvalSymlinks(root); err == nil && realRoot != root {
		fmt.Printf("Resolved symlink to: %s\n", realRoot)
		return realRoot
	}

	// If still unresolved, try os.Readlink directly (covers WSL cases)
	if target, err := os.Readlink(root); err == nil {
		resolved := resolveSymlinkTarget(root, target)
		if resolved != root {
			fmt.Printf("Resolved symlink to: %s\n", resolved)
		}
		return resolved
	}

	// Nothing to resolve
	return root
}

// resolveSymlinkTarget handles different symlink target formats
func resolveSymlinkTarget(root, target string) string {
	// Handle WSL-style paths: /x/path → X:\path
	if len(target) > 3 && target[0] == '/' && target[2] == '/' {
		driveLetter := strings.ToUpper(string(target[1]))
		return driveLetter + ":" + strings.ReplaceAll(target[2:], "/", "\\")
	}

	// Handle relative symlinks
	if !filepath.IsAbs(target) {
		return filepath.Join(filepath.Dir(root), target)
	}

	return target
}

func (cfg *Config) shouldSkipDir(name string) bool {
	// Check if explicitly included
	if _, included := cfg.includeSet[name]; included {
		return false
	}

	// Check if in skip set
	if _, skip := cfg.skipSet[name]; skip {
		return true
	}

	// Skip hidden directories except .git
	if strings.HasPrefix(name, ".") && name != ".git" {
		return true
	}

	return false
}

const (
	progressUpdateInterval = 500 * time.Millisecond
)

// findGitRepos recursively searches for git repositories starting from root.
// It respects the skip/include configuration and avoids symlink loops.
// Progress is printed to stdout during scanning.
func findGitRepos(root string, cfg *Config) ([]RepoInfo, error) {
	scanner := &repoScanner{
		cfg:        cfg,
		visited:    make(map[string]bool),
		output:     os.Stdout,
		lastUpdate: time.Now(),
	}

	err := filepath.WalkDir(root, scanner.walkFunc(root))
	scanner.printFinal()

	return scanner.repos, err
}

type repoScanner struct {
	cfg        *Config
	repos      []RepoInfo
	visited    map[string]bool
	processed  int
	skipped    int
	output     io.Writer
	lastUpdate time.Time
}

func (s *repoScanner) walkFunc(root string) func(string, os.DirEntry, error) error {
	return func(path string, d os.DirEntry, err error) error {
		if err != nil {
			// Log error but continue scanning
			return nil
		}

		if !d.IsDir() || path == root {
			return nil
		}

		name := d.Name()

		// Check if directory should be skipped
		if s.cfg.shouldSkipDir(name) {
			s.skipped++
			return filepath.SkipDir
		}

		// Avoid symlink loops
		if s.isVisited(path) {
			s.skipped++
			return filepath.SkipDir
		}

		s.processed++
		s.printProgress()

		// Check if this is a git repository
		if s.isGitRepo(path) {
			rel, _ := filepath.Rel(root, path)
			s.repos = append(s.repos, RepoInfo{Path: path, RelPath: rel})
			return filepath.SkipDir
		}

		return nil
	}
}

func (s *repoScanner) isVisited(path string) bool {
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		return false
	}

	if s.visited[real] {
		return true
	}

	s.visited[real] = true
	return false
}

func (s *repoScanner) isGitRepo(path string) bool {
	_, err := os.Stat(filepath.Join(path, ".git"))
	return err == nil
}

func (s *repoScanner) printProgress() {
	if time.Since(s.lastUpdate) > progressUpdateInterval {
		fmt.Fprintf(s.output, "Scanned %d dirs (skipped %d)...\r", s.processed, s.skipped)
		s.lastUpdate = time.Now()
	}
}

func (s *repoScanner) printFinal() {
	fmt.Fprintf(s.output, "Scanned %d dirs (skipped %d).\n", s.processed, s.skipped)
}
