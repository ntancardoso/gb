package core

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type RepoInfo struct {
	Path    string
	RelPath string
}

func resolveRoot(root string) string {
	if realRoot, err := filepath.EvalSymlinks(root); err == nil && realRoot != root {
		fmt.Printf("Resolved symlink to: %s\n", realRoot)
		return realRoot
	}

	if target, err := os.Readlink(root); err == nil {
		resolved := resolveSymlinkTarget(root, target)
		if resolved != root {
			fmt.Printf("Resolved symlink to: %s\n", resolved)
		}
		return resolved
	}

	return root
}

func resolveSymlinkTarget(root, target string) string {
	if len(target) > 3 && target[0] == '/' && target[2] == '/' {
		driveLetter := strings.ToUpper(string(target[1]))
		return driveLetter + ":" + strings.ReplaceAll(target[2:], "/", "\\")
	}

	if !filepath.IsAbs(target) {
		return filepath.Join(filepath.Dir(root), target)
	}

	return target
}

func (cfg *Config) shouldSkipDir(name string) bool {
	if _, included := cfg.includeSet[name]; included {
		return false
	}

	if strings.HasPrefix(name, ".") && name != ".git" {
		return true
	}

	performanceSkipDirs := []string{
		"node_modules", "vendor", "__pycache__", ".pytest_cache",
		"build", "dist", "out", "target", "bin", "obj",
		".next", "coverage", ".nyc_output", ".tox",
		".venv", "venv", ".env", "env",
	}

	for _, skipDir := range performanceSkipDirs {
		if name == skipDir {
			return true
		}
	}

	return false
}

const (
	progressUpdateInterval = 500 * time.Millisecond
)

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
			return nil
		}

		if !d.IsDir() || path == root {
			return nil
		}

		name := d.Name()

		if s.cfg.shouldSkipDir(name) {
			s.skipped++
			return filepath.SkipDir
		}

		if s.isVisited(path) {
			s.skipped++
			return filepath.SkipDir
		}

		s.processed++
		s.printProgress()
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
		_, _ = fmt.Fprintf(s.output, "Scanned %d dirs (skipped %d)...\r", s.processed, s.skipped)
		s.lastUpdate = time.Now()
	}
}

func (s *repoScanner) printFinal() {
	_, _ = fmt.Fprintf(s.output, "Scanned %d dirs (skipped %d).\n", s.processed, s.skipped)
}
