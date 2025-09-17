package core

import (
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"
)

type BranchResult struct {
	RelPath string
	Branch  string
	Error   error
}

func getBranch(path string) (string, error) {
	cmds := [][]string{
		{"git", "branch", "--show-current"},
		{"git", "rev-parse", "--abbrev-ref", "HEAD"},
	}
	for _, args := range cmds {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = path
		if out, err := cmd.Output(); err == nil {
			b := strings.TrimSpace(string(out))
			if b != "" && b != "HEAD" {
				return b, nil
			}
		}
	}
	cmd := exec.Command("git", "log", "-1", "--oneline")
	cmd.Dir = path
	if err := cmd.Run(); err != nil {
		return "no commits", nil
	}
	return "detached", nil
}

func listAllBranches(root string, workers int) {
	fmt.Printf("Discovering repos in %s...\n", root)
	repos, err := findGitRepos(root)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
	if len(repos) == 0 {
		fmt.Println("No repos found")
		return
	}

	repoCh := make(chan RepoInfo, len(repos))
	resCh := make(chan BranchResult, len(repos))

	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for r := range repoCh {
				branch, err := getBranch(r.Path)
				resCh <- BranchResult{RelPath: r.RelPath, Branch: branch, Error: err}
			}
		}()
	}
	go func() {
		for _, r := range repos {
			repoCh <- r
		}
		close(repoCh)
	}()
	go func() {
		wg.Wait()
		close(resCh)
	}()

	branchRepos := make(map[string][]string)
	for res := range resCh {
		key := res.Branch
		if res.Error != nil {
			key = "error"
		}
		branchRepos[key] = append(branchRepos[key], res.RelPath)
	}

	var branches []string
	for b := range branchRepos {
		branches = append(branches, b)
	}
	sort.Strings(branches)

	for _, b := range branches {
		fmt.Printf("Branch: %s\n", b)
		fmt.Println("-----------------")
		sort.Strings(branchRepos[b])
		for _, repo := range branchRepos[b] {
			fmt.Println(repo)
		}
		fmt.Println("=================")
	}
}
