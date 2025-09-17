package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

var (
	// Configuration variables
	defaultWorkers = 10

	// Common directories to skip during repository discovery
	skipDirs = []string{
		"vendor", "node_modules", ".vscode", ".idea", "build", "dist", "out",
		"target", "bin", "obj", ".next", "coverage", ".nyc_output", "__pycache__",
		".pytest_cache", ".tox", ".venv", "venv", ".env", "env",
	}
)

func checkGitInstalled() {
	if _, err := exec.LookPath("git"); err != nil {
		fmt.Fprintf(os.Stderr, "Error: Git is required but not found in PATH\n")
		fmt.Fprintf(os.Stderr, "Please install Git and ensure it's available in your PATH\n")
		os.Exit(1)
	}
}

func main() {
	checkGitInstalled()

	var listBranches = flag.Bool("list", false, "List all branches found in repositories")
	var workers = flag.Int("workers", defaultWorkers, "Number of concurrent workers")

	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "Usage: %s [options] <branch_name>\n", os.Args[0])
		fmt.Println("Options:")
		fmt.Println("  -list         List all branches found in repositories without switching")
		fmt.Printf("  -workers N    Number of concurrent workers (default: %d)\n", defaultWorkers)
		fmt.Println("Examples:")
		fmt.Printf("  %s main\n", os.Args[0])
		fmt.Printf("  %s -list\n", os.Args[0])
		fmt.Printf("  %s -list -workers 50\n", os.Args[0])
		fmt.Printf("  %s main -workers 5\n", os.Args[0])
	}
	flag.Parse()

	root, _ := os.Getwd()

	// Resolve symlinks/junctions
	if realRoot, err := filepath.EvalSymlinks(root); err == nil && realRoot != root {
		root = realRoot
		fmt.Printf("Resolved symlink to: %s\n", root)
	} else if target, err := os.Readlink(root); err == nil {
		// Handle WSL-style paths and relative paths
		if len(target) > 3 && target[0] == '/' && target[2] == '/' {
			// Convert /x/path to X:\path for any drive letter
			driveLetter := strings.ToUpper(string(target[1]))
			windowsPath := driveLetter + ":" + strings.ReplaceAll(target[2:], "/", "\\")
			root = windowsPath
		} else if !filepath.IsAbs(target) {
			root = filepath.Join(filepath.Dir(root), target)
		} else {
			root = target
		}
		fmt.Printf("Resolved symlink to: %s\n", root)
	}

	if *listBranches {
		listAllBranches(root, *workers)
		return
	}

	if flag.NArg() < 1 {
		flag.Usage()
		os.Exit(1)
	}

	targetBranch := flag.Arg(0)
	switchBranches(root, targetBranch, *workers)
}

type RepoInfo struct {
	Path    string
	RelPath string
}

type BranchResult struct {
	RelPath string
	Branch  string
	Error   error
}

type SwitchResult struct {
	RelPath string
	Success bool
	Error   string
}

func shouldSkipDir(name string) bool {
	// Check exact matches
	for _, skip := range skipDirs {
		if name == skip {
			return true
		}
	}
	// Skip hidden directories except .git
	if strings.HasPrefix(name, ".") && name != ".git" {
		return true
	}
	return false
}

func findGitRepos(root string) ([]RepoInfo, error) {
	var repos []RepoInfo
	visitedDirs := make(map[string]bool)
	processed := 0
	skipped := 0

	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}

		if d.IsDir() && path != root {
			name := d.Name()

			// Early skip for common large directories
			if shouldSkipDir(name) {
				skipped++
				return filepath.SkipDir
			}

			// Prevent infinite loops
			if realPath, err := filepath.EvalSymlinks(path); err == nil {
				if visitedDirs[realPath] {
					skipped++
					return filepath.SkipDir
				}
				visitedDirs[realPath] = true
			}

			processed++
			if processed%1000 == 0 {
				fmt.Printf("Scanned %d directories (skipped %d)...\r", processed, skipped)
			}

			// Check for git repo - but don't skip subdirs (allow nested repos)
			if _, err := os.Stat(filepath.Join(path, ".git")); err == nil {
				relPath, _ := filepath.Rel(root, path)
				repos = append(repos, RepoInfo{
					Path:    path,
					RelPath: relPath,
				})
			}
		}
		return nil
	})

	fmt.Printf("Scanned %d directories (skipped %d).\n", processed, skipped)
	return repos, err
}

func listAllBranches(root string, workers int) {
	fmt.Printf("Discovering git repositories in %s...\n", root)
	repos, err := findGitRepos(root)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error finding repositories: %v\n", err)
		os.Exit(1)
	}

	if len(repos) == 0 {
		fmt.Println("No git repositories found")
		return
	}

	fmt.Printf("Found %d repositories, checking branches with %d workers...\n\n", len(repos), workers)

	// Create worker pool
	repoChan := make(chan RepoInfo, len(repos))
	resultChan := make(chan BranchResult, len(repos))

	// Start workers
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for repo := range repoChan {
				var branch string
				var repoErr error

				// Try multiple methods to get current branch
				cmd := exec.Command("git", "branch", "--show-current")
				cmd.Dir = repo.Path
				if output, err := cmd.Output(); err == nil {
					branch = strings.TrimSpace(string(output))
				}

				// If empty, try alternative method for detached HEAD
				if branch == "" {
					cmd = exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
					cmd.Dir = repo.Path
					if output, err := cmd.Output(); err == nil {
						branch = strings.TrimSpace(string(output))
						if branch == "HEAD" {
							branch = "detached"
						}
					} else {
						repoErr = err
					}
				}

				// If still empty, check if repo has any commits
				if branch == "" && repoErr == nil {
					cmd = exec.Command("git", "log", "-1", "--oneline")
					cmd.Dir = repo.Path
					if err := cmd.Run(); err != nil {
						branch = "no commits"
					}
				}

				resultChan <- BranchResult{
					RelPath: repo.RelPath,
					Branch:  branch,
					Error:   repoErr,
				}
			}
		}()
	}

	// Send work
	go func() {
		for _, repo := range repos {
			repoChan <- repo
		}
		close(repoChan)
	}()

	// Close result channel when all workers done
	go func() {
		wg.Wait()
		close(resultChan)
	}()

	// Collect results
	branchRepos := make(map[string][]string)
	for result := range resultChan {
		if result.Error != nil {
			branchRepos["error"] = append(branchRepos["error"], result.RelPath+" ("+result.Error.Error()+")")
		} else if result.Branch != "" {
			branchRepos[result.Branch] = append(branchRepos[result.Branch], result.RelPath)
		} else {
			branchRepos["unknown"] = append(branchRepos["unknown"], result.RelPath)
		}
	}

	// Sort and display results
	var branches []string
	for branch := range branchRepos {
		branches = append(branches, branch)
	}
	sort.Strings(branches)

	for _, branch := range branches {
		fmt.Printf("Branch: %s\n", branch)
		fmt.Println("-----------------")
		sort.Strings(branchRepos[branch])
		for _, repo := range branchRepos[branch] {
			fmt.Println(repo)
		}
		fmt.Println("=================")
	}
}

func switchBranches(root, targetBranch string, workers int) {
	repos, err := findGitRepos(root)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error finding repositories: %v\n", err)
		os.Exit(1)
	}

	if len(repos) == 0 {
		fmt.Println("No git repositories found")
		return
	}

	fmt.Printf("Found %d repositories, switching to %s with %d workers...\n\n", len(repos), targetBranch, workers)

	// Create channels for work distribution
	repoChan := make(chan RepoInfo, len(repos))
	resultChan := make(chan SwitchResult, len(repos))

	// Progress tracking
	var progressMu sync.Mutex
	processed := 0

	// Start workers
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for repo := range repoChan {
				result := processSingleRepo(repo, targetBranch)

				// Thread-safe progress update
				progressMu.Lock()
				processed++
				fmt.Printf("[%d/%d] %s\n", processed, len(repos), result.RelPath)
				if !result.Success {
					fmt.Printf("  Error: %s\n", result.Error)
				} else {
					fmt.Printf("  Successfully switched to %s\n", targetBranch)
				}
				progressMu.Unlock()

				resultChan <- result
			}
		}(i)
	}

	// Send work to workers
	go func() {
		for _, repo := range repos {
			repoChan <- repo
		}
		close(repoChan)
	}()

	// Close result channel when all workers are done
	go func() {
		wg.Wait()
		close(resultChan)
	}()

	// Collect results
	var count, errors int
	var failedRepos []string
	for result := range resultChan {
		if result.Success {
			count++
		} else {
			errors++
			failedRepos = append(failedRepos, result.RelPath+" ("+result.Error+")")
		}
	}

	fmt.Println("\n--------------------------------------------------")
	fmt.Printf("Operation complete:\n")
	fmt.Printf("Successfully switched %d repositories to %s\n", count, targetBranch)

	if errors > 0 {
		fmt.Printf("\nFailed repositories (%d):\n", errors)
		sort.Strings(failedRepos)
		for _, repo := range failedRepos {
			fmt.Printf("  %s\n", repo)
		}
		os.Exit(1)
	}
}

func processSingleRepo(repo RepoInfo, targetBranch string) SwitchResult {
	// Check if local branch exists
	branchExists := false
	cmd := exec.Command("git", "show-ref", "--verify", "--quiet", "refs/heads/"+targetBranch)
	cmd.Dir = repo.Path
	if cmd.Run() == nil {
		branchExists = true
	}

	if !branchExists {
		// Check remote branch
		cmd = exec.Command("git", "ls-remote", "--exit-code", "--heads", "origin", targetBranch)
		cmd.Dir = repo.Path
		if cmd.Run() == nil {
			branchExists = true

			// Check if shallow repo
			shallowCmd := exec.Command("git", "rev-parse", "--is-shallow-repository")
			shallowCmd.Dir = repo.Path
			shallow := shallowCmd.Run() == nil

			// Fetch the branch
			fetchCmd := exec.Command("git", "fetch", "origin", targetBranch+":"+targetBranch)
			fetchCmd.Dir = repo.Path
			if shallow {
				fetchCmd.Args = append(fetchCmd.Args, "--depth=1")
			}
			if err := fetchCmd.Run(); err != nil {
				return SwitchResult{
					RelPath: repo.RelPath,
					Success: false,
					Error:   "fetch failed",
				}
			}
		} else {
			return SwitchResult{
				RelPath: repo.RelPath,
				Success: false,
				Error:   "branch not found",
			}
		}
	}

	if branchExists {
		// Try to switch to existing branch
		switchCmd := exec.Command("git", "switch", targetBranch)
		switchCmd.Dir = repo.Path
		if err := switchCmd.Run(); err == nil {
			return SwitchResult{
				RelPath: repo.RelPath,
				Success: true,
				Error:   "",
			}
		} else {
			// Try to create tracking branch
			trackCmd := exec.Command("git", "switch", "-c", targetBranch, "--track", "origin/"+targetBranch)
			trackCmd.Dir = repo.Path
			if err := trackCmd.Run(); err == nil {
				return SwitchResult{
					RelPath: repo.RelPath,
					Success: true,
					Error:   "",
				}
			} else {
				return SwitchResult{
					RelPath: repo.RelPath,
					Success: false,
					Error:   "switch failed",
				}
			}
		}
	}

	return SwitchResult{
		RelPath: repo.RelPath,
		Success: false,
		Error:   "unknown error",
	}
}
