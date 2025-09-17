package core

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type RepoInfo struct {
	Path    string
	RelPath string
}

// resolveRoot normalizes root path for Windows junctions and WSL symlinks.
func resolveRoot(root string) string {
	// First try EvalSymlinks (handles junctions/symlinks generally)
	if realRoot, err := filepath.EvalSymlinks(root); err == nil && realRoot != root {
		fmt.Printf("Resolved symlink to: %s\n", realRoot)
		return realRoot
	}

	// If still unresolved, try os.Readlink directly (covers WSL cases)
	if target, err := os.Readlink(root); err == nil {
		if len(target) > 3 && target[0] == '/' && target[2] == '/' {
			// Convert /x/path → X:\path (drive letter conversion)
			driveLetter := strings.ToUpper(string(target[1]))
			windowsPath := driveLetter + ":" + strings.ReplaceAll(target[2:], "/", "\\")
			fmt.Printf("Resolved symlink to: %s\n", windowsPath)
			return windowsPath
		}
		if !filepath.IsAbs(target) {
			resolved := filepath.Join(filepath.Dir(root), target)
			fmt.Printf("Resolved symlink to: %s\n", resolved)
			return resolved
		}
		fmt.Printf("Resolved symlink to: %s\n", target)
		return target
	}

	// Nothing to resolve
	return root
}

func buildSkipSet(skipDirs []string) map[string]struct{} {
	m := make(map[string]struct{}, len(skipDirs))
	for _, s := range skipDirs {
		m[s] = struct{}{}
	}
	return m
}

func shouldSkipDir(name string, includeSet map[string]struct{}) bool {
	if _, inc := includeSet[name]; inc {
		return false
	}
	if _, found := skipSet[name]; found {
		return true
	}
	if strings.HasPrefix(name, ".") && name != ".git" {
		return true
	}
	return false
}

func findGitRepos(root string) ([]RepoInfo, error) {
	var repos []RepoInfo
	visited := make(map[string]bool)
	processed, skipped := 0, 0
	lastPrint := time.Now()

	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() && path != root {
			name := d.Name()
			if shouldSkipDir(name, includeSet) {
				skipped++
				return filepath.SkipDir
			}
			if real, err := filepath.EvalSymlinks(path); err == nil {
				if visited[real] {
					skipped++
					return filepath.SkipDir
				}
				visited[real] = true
			}
			processed++
			if time.Since(lastPrint) > 500*time.Millisecond {
				fmt.Printf("Scanned %d dirs (skipped %d)...\r", processed, skipped)
				lastPrint = time.Now()
			}
			if _, err := os.Stat(filepath.Join(path, ".git")); err == nil {
				rel, _ := filepath.Rel(root, path)
				repos = append(repos, RepoInfo{Path: path, RelPath: rel})
				return filepath.SkipDir
			}
		}
		return nil
	})
	fmt.Printf("Scanned %d dirs (skipped %d).\n", processed, skipped)
	return repos, err
}
