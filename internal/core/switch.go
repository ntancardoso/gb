package core

import (
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"
)

type SwitchResult struct {
	RelPath string
	Success bool
	Error   string
}

type ProgressState struct {
	repos      []RepoInfo
	statuses   []string // indexed by repo order
	completed  []bool   // track completion
	mu         sync.Mutex
	totalRepos int
	linesDrawn int
}

func NewProgressState(repos []RepoInfo) *ProgressState {
	statuses := make([]string, len(repos))
	completed := make([]bool, len(repos))
	for i := range statuses {
		statuses[i] = "waiting"
	}
	return &ProgressState{
		repos:      repos,
		statuses:   statuses,
		completed:  completed,
		totalRepos: len(repos),
	}
}

func (ps *ProgressState) UpdateStatus(relPath, status, errorMsg string) {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	// Find repo index
	repoIndex := -1
	for i, repo := range ps.repos {
		if repo.RelPath == relPath {
			repoIndex = i
			break
		}
	}
	if repoIndex == -1 {
		return
	}

	// Update status
	switch status {
	case "processing":
		ps.statuses[repoIndex] = "processing..."
	case "ok":
		ps.statuses[repoIndex] = "completed"
		ps.completed[repoIndex] = true
	case "error":
		ps.statuses[repoIndex] = fmt.Sprintf("failed: %s", errorMsg)
		ps.completed[repoIndex] = true
	}

	ps.render()
}

func (ps *ProgressState) render() {
	// Move cursor up to overwrite previous output
	if ps.linesDrawn > 0 {
		fmt.Printf("\033[%dA", ps.linesDrawn)
	}

	// Count statuses
	waiting, processing, ok, failed := 0, 0, 0, 0

	lines := 0
	fmt.Printf("Progress: Switching branches...\n")
	lines++

	// Show first few repos with status
	maxShow := 30
	if len(ps.repos) < maxShow {
		maxShow = len(ps.repos)
	}

	for i := 0; i < maxShow; i++ {
		var icon string
		status := ps.statuses[i]

		switch {
		case status == "waiting":
			icon = "⏳"
			waiting++
		case strings.HasPrefix(status, "processing"):
			icon = "🔄"
			processing++
		case status == "completed":
			icon = "✅"
			ok++
		case strings.HasPrefix(status, "failed:"):
			icon = "❌"
			failed++
		default:
			icon = "⏳"
			waiting++
		}

		fmt.Printf("[%d] %s %s - %s\033[K\n", i+1, icon, ps.repos[i].RelPath, status)
		lines++
	}

	// Count remaining repos if we're showing limited view
	if len(ps.repos) > maxShow {
		for i := maxShow; i < len(ps.repos); i++ {
			status := ps.statuses[i]
			switch {
			case status == "waiting":
				waiting++
			case strings.HasPrefix(status, "processing"):
				processing++
			case status == "completed":
				ok++
			case strings.HasPrefix(status, "failed:"):
				failed++
			default:
				waiting++
			}
		}

		fmt.Printf("... and %d more repos\n", len(ps.repos)-maxShow)
		lines++
	}

	fmt.Printf("Status: %d waiting, %d processing, %d done, %d failed (%d/%d)\n",
		waiting, processing, ok, failed, ok+failed, ps.totalRepos)
	lines++

	// Clear any remaining lines from previous render
	if lines < ps.linesDrawn {
		for i := lines; i < ps.linesDrawn; i++ {
			fmt.Printf("\033[K\n") // Clear line
		}
		fmt.Printf("\033[%dA", ps.linesDrawn-lines) // Move back up
	}

	ps.linesDrawn = lines
}

func switchBranches(root, target string, workers int) {
	repos, err := findGitRepos(root)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
	if len(repos) == 0 {
		fmt.Println("No repos found")
		return
	}

	fmt.Printf("Found %d repos, switching to %s with %d workers...\n", len(repos), target, workers)

	// Initialize progress state
	progress := NewProgressState(repos)
	progress.render() // Initial render

	repoCh := make(chan RepoInfo, len(repos))
	resCh := make(chan SwitchResult, len(repos))

	var wg sync.WaitGroup

	// Workers
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for r := range repoCh {
				// Update to processing
				progress.UpdateStatus(r.RelPath, "processing", "")

				// Process repo
				res := processSingleRepo(r, target)

				// Update completion status
				if res.Success {
					progress.UpdateStatus(r.RelPath, "ok", "")
				} else {
					progress.UpdateStatus(r.RelPath, "error", res.Error)
				}

				resCh <- res
			}
		}()
	}

	// Send repos to workers
	go func() {
		for _, r := range repos {
			repoCh <- r
		}
		close(repoCh)
	}()

	// Wait for workers and close results
	go func() {
		wg.Wait()
		close(resCh)
	}()

	// Collect results
	var ok, fail int
	var failed []string
	for res := range resCh {
		if res.Success {
			ok++
		} else {
			fail++
			failed = append(failed, res.RelPath+" ("+res.Error+")")
		}
	}

	// Move past the progress display
	fmt.Printf("\n\n--- Summary ---\n")
	fmt.Printf("Switched %d repos to %s\n", ok, target)
	if fail > 0 {
		fmt.Printf("Failed %d repos:\n", fail)
		sort.Strings(failed)
		for _, f := range failed {
			fmt.Printf("  %s\n", f)
		}
		os.Exit(1)
	}
}

func processSingleRepo(repo RepoInfo, targetBranch string) SwitchResult {
	// Check if local branch exists
	localCheck := exec.Command("git", "show-ref", "--verify", "--quiet", "refs/heads/"+targetBranch)
	localCheck.Dir = repo.Path
	branchExists := localCheck.Run() == nil

	if !branchExists {
		// Check if branch exists remotely
		remoteCheck := exec.Command("git", "ls-remote", "--exit-code", "--heads", "origin", targetBranch)
		remoteCheck.Dir = repo.Path
		if remoteCheck.Run() == nil {
			// Detect shallow repo
			out, err := exec.Command("git", "rev-parse", "--is-shallow-repository").Output()
			isShallow := err == nil && strings.TrimSpace(string(out)) == "true"

			// Fetch branch
			args := []string{"fetch", "origin"}
			if isShallow {
				args = append(args, "--depth=1")
			}
			args = append(args, targetBranch)

			fetchCmd := exec.Command("git", args...)
			fetchCmd.Dir = repo.Path
			if out, err := fetchCmd.CombinedOutput(); err != nil {
				return SwitchResult{
					RelPath: repo.RelPath,
					Success: false,
					Error:   "fetch failed: " + string(out),
				}
			}
			branchExists = true
		} else {
			return SwitchResult{RelPath: repo.RelPath, Success: false, Error: "branch not found"}
		}
	}

	// Try normal switch
	switchCmd := exec.Command("git", "switch", targetBranch)
	switchCmd.Dir = repo.Path
	if out, err := switchCmd.CombinedOutput(); err == nil {
		return SwitchResult{RelPath: repo.RelPath, Success: true}
	} else {
		// Try to create tracking branch from remote
		trackCmd := exec.Command("git", "switch", "-c", targetBranch, "--track", "origin/"+targetBranch)
		trackCmd.Dir = repo.Path
		if out2, err2 := trackCmd.CombinedOutput(); err2 == nil {
			return SwitchResult{RelPath: repo.RelPath, Success: true}
		} else {
			return SwitchResult{
				RelPath: repo.RelPath,
				Success: false,
				Error:   "switch failed: " + string(out) + string(out2),
			}
		}
	}
}
