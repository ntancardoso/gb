package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

func main() {
	var listBranches = flag.Bool("list", false, "List all branches found in repositories")

	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "Usage: %s [options] <branch_name>\n", os.Args[0])
		fmt.Println("Options:")
		fmt.Println("  -list    List all branches found in repositories without switching")
		fmt.Println("Examples:")
		fmt.Printf("  %s main\n", os.Args[0])
		fmt.Printf("  %s -list\n", os.Args[0])
	}
	flag.Parse()

	root, _ := os.Getwd()

	// Resolve symlinks/junctions
	if realRoot, err := filepath.EvalSymlinks(root); err == nil && realRoot != root {
		root = realRoot
		fmt.Printf("Resolved symlink to: %s\n", root)
	} else if target, err := os.Readlink(root); err == nil {
		// Handle WSL-style paths and relative paths
		if strings.HasPrefix(target, "/c/") {
			root = "C:" + strings.ReplaceAll(target[2:], "/", "\\")
		} else if !filepath.IsAbs(target) {
			root = filepath.Join(filepath.Dir(root), target)
		} else {
			root = target
		}
		fmt.Printf("Resolved symlink to: %s\n", root)
	}

	if *listBranches {
		listAllBranches(root)
		return
	}

	if flag.NArg() < 1 {
		flag.Usage()
		os.Exit(1)
	}

	targetBranch := flag.Arg(0)
	switchBranches(root, targetBranch)
}

func listAllBranches(root string) {
	branchRepos := make(map[string][]string)
	visitedDirs := make(map[string]bool)

	filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || path == root {
			return nil
		}

		if d.IsDir() {
			// Prevent infinite loops
			if realPath, err := filepath.EvalSymlinks(path); err == nil {
				if visitedDirs[realPath] {
					return filepath.SkipDir
				}
				visitedDirs[realPath] = true
			}

			if d.Name() == "vendor" || d.Name() == "node_modules" {
				return filepath.SkipDir
			}

			if _, err := os.Stat(filepath.Join(path, ".git")); err == nil {
				cmd := exec.Command("git", "branch", "--show-current")
				cmd.Dir = path
				if output, err := cmd.Output(); err == nil {
					branch := strings.TrimSpace(string(output))
					if branch != "" {
						relPath, _ := filepath.Rel(root, path)
						branchRepos[branch] = append(branchRepos[branch], relPath)
					}
				}
			}
		}
		return nil
	})

	// Sort and display results
	var branches []string
	for branch := range branchRepos {
		branches = append(branches, branch)
	}
	sort.Strings(branches)

	for _, branch := range branches {
		fmt.Printf("Branch: %s\n", branch)
		fmt.Println("=================================")
		sort.Strings(branchRepos[branch])
		for _, repo := range branchRepos[branch] {
			fmt.Println(repo)
		}
		fmt.Println("---------------------------------")
	}
}

func switchBranches(root, targetBranch string) {
	var count, errors int
	var failedRepos []string
	visitedDirs := make(map[string]bool)

	filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || path == root {
			return nil
		}

		if d.IsDir() {
			// Prevent infinite loops
			if realPath, err := filepath.EvalSymlinks(path); err == nil {
				if visitedDirs[realPath] {
					return filepath.SkipDir
				}
				visitedDirs[realPath] = true
			}

			if d.Name() == "vendor" || d.Name() == "node_modules" {
				return filepath.SkipDir
			}

			if _, err := os.Stat(filepath.Join(path, ".git")); err == nil {
				relPath, _ := filepath.Rel(root, path)
				fmt.Println("--------------------------------------------------")
				fmt.Printf("Processing: %s\n", relPath)

				branchExists := false
				cmd := exec.Command("git", "show-ref", "--verify", "--quiet", "refs/heads/"+targetBranch)
				cmd.Dir = path
				if cmd.Run() == nil {
					branchExists = true
					fmt.Printf("Found local branch: %s\n", targetBranch)
				}

				if !branchExists {
					fmt.Printf("Checking remote for branch %s...\n", targetBranch)
					cmd = exec.Command("git", "ls-remote", "--exit-code", "--heads", "origin", targetBranch)
					cmd.Dir = path
					if cmd.Run() == nil {
						branchExists = true
						fmt.Printf("Found remote branch: origin/%s\n", targetBranch)

						shallowCmd := exec.Command("git", "rev-parse", "--is-shallow-repository")
						shallowCmd.Dir = path
						shallow := shallowCmd.Run() == nil

						fetchCmd := exec.Command("git", "fetch", "origin", targetBranch+":"+targetBranch)
						if shallow {
							fmt.Println("Repository is shallow - fetching specific branch...")
							fetchCmd.Args = append(fetchCmd.Args, "--depth=1")
						}
						if err := fetchCmd.Run(); err != nil {
							fmt.Printf("Fetch failed: %v\n", err)
							errors++
							failedRepos = append(failedRepos, relPath+" (fetch failed)")
						}
					} else {
						fmt.Printf("Remote branch origin/%s not found\n", targetBranch)
						errors++
						failedRepos = append(failedRepos, relPath+" (branch not found)")
					}
				}

				if branchExists {
					switchCmd := exec.Command("git", "switch", targetBranch)
					switchCmd.Dir = path
					if err := switchCmd.Run(); err == nil {
						count++
						fmt.Printf("Successfully switched to %s\n", targetBranch)
					} else {
						trackCmd := exec.Command("git", "switch", "-c", targetBranch, "--track", "origin/"+targetBranch)
						trackCmd.Dir = path
						if err := trackCmd.Run(); err == nil {
							count++
							fmt.Printf("Successfully created tracking branch %s\n", targetBranch)
						} else {
							errors++
							failedRepos = append(failedRepos, relPath+" (switch failed)")
							fmt.Printf("Failed to switch/create branch: %v\n", err)
						}
					}
				}
				fmt.Println()
			}
		}
		return nil
	})

	fmt.Println("--------------------------------------------------")
	fmt.Printf("Operation complete:\n")
	fmt.Printf("Successfully switched %d repositories to %s\n", count, targetBranch)

	if errors > 0 {
		fmt.Printf("\nFailed repositories (%d):\n", errors)
		for _, repo := range failedRepos {
			fmt.Printf("  %s\n", repo)
		}
		os.Exit(1)
	}
}
