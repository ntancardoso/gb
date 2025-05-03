package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

func main() {
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "Usage: %s <branch_name>\n", os.Args[0])
		fmt.Println("Example:")
		fmt.Printf("  %s main\n", os.Args[0])
	}
	flag.Parse()

	if flag.NArg() < 1 {
		flag.Usage()
		os.Exit(1)
	}

	targetBranch := flag.Arg(0)
	root, _ := os.Getwd()
	var count, errors int

	filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || path == root {
			return nil
		}

		if d.IsDir() {
			if d.Name() == "vendor" || d.Name() == "node_modules" {
				return filepath.SkipDir
			}

			if _, err := os.Stat(filepath.Join(path, ".git")); err == nil {
				fmt.Println("--------------------------------------------------")
				fmt.Printf("Processing: %s\n", filepath.Base(path))

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
						}
					} else {
						fmt.Printf("Remote branch origin/%s not found\n", targetBranch)
						errors++
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
		fmt.Printf("Failed to switch %d repositories\n", errors)
		os.Exit(1)
	}
}
