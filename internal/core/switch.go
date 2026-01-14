package core

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"
)

const (
	maxDisplayedRepos = 30
	statusWaiting     = "waiting"
	statusProcessing  = "processing"
	statusCompleted   = "completed"
	statusFailed      = "failed"
)

type SwitchResult struct {
	RelPath string
	Success bool
	Error   string
}

type repoStatus struct {
	state   string
	message string
}

type ProgressState struct {
	repos        []RepoInfo
	statuses     []repoStatus
	mu           sync.Mutex
	totalRepos   int
	writer       io.Writer
	supportsANSI bool
	linesDrawn   int
}

func NewProgressState(repos []RepoInfo) *ProgressState {
	statuses := make([]repoStatus, len(repos))
	for i := range statuses {
		statuses[i] = repoStatus{state: statusWaiting}
	}

	return &ProgressState{
		repos:        repos,
		statuses:     statuses,
		totalRepos:   len(repos),
		writer:       os.Stdout,
		supportsANSI: supportsANSI(),
	}
}

func supportsANSI() bool {
	if fileInfo, _ := os.Stdout.Stat(); (fileInfo.Mode() & os.ModeCharDevice) == 0 {
		return false
	}

	term := os.Getenv("TERM")
	if term != "" && term != "dumb" {
		return true
	}
	if os.Getenv("WT_SESSION") != "" {
		return true
	}

	return false
}

func (ps *ProgressState) UpdateStatus(relPath, status, errorMsg string) {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	repoIndex := ps.findRepoIndex(relPath)
	if repoIndex == -1 {
		return
	}

	switch status {
	case statusProcessing:
		ps.statuses[repoIndex] = repoStatus{state: statusProcessing}
	case statusCompleted:
		ps.statuses[repoIndex] = repoStatus{state: statusCompleted}
	case statusFailed:
		ps.statuses[repoIndex] = repoStatus{state: statusFailed, message: errorMsg}
	}

	ps.render()
}

func (ps *ProgressState) findRepoIndex(relPath string) int {
	for i, repo := range ps.repos {
		if repo.RelPath == relPath {
			return i
		}
	}
	return -1
}

func (ps *ProgressState) render() {
	if ps.supportsANSI {
		ps.renderANSI()
	} else {
		ps.renderSimple()
	}
}

func (ps *ProgressState) renderANSI() {
	if ps.linesDrawn > 0 {
		_, _ = fmt.Fprintf(ps.writer, "\033[%dA\r", ps.linesDrawn)
	}

	lines := 0
	waiting, processing, completed, failed := ps.countStatuses()

	_, _ = fmt.Fprintf(ps.writer, "\033[KProgress: Switching branches...\n")
	lines++

	displayCount := min(maxDisplayedRepos, len(ps.repos))
	for i := 0; i < displayCount; i++ {
		icon := ps.getStatusIcon(ps.statuses[i].state)
		statusText := ps.formatStatus(ps.statuses[i])
		_, _ = fmt.Fprintf(ps.writer, "\033[K[%d] %s %s - %s\n", i+1, icon, ps.repos[i].RelPath, statusText)
		lines++
	}

	if len(ps.repos) > maxDisplayedRepos {
		_, _ = fmt.Fprintf(ps.writer, "\033[K... and %d more repos\n", len(ps.repos)-maxDisplayedRepos)
		lines++
	}

	_, _ = fmt.Fprintf(ps.writer, "\033[KStatus: %d waiting, %d processing, %d done, %d failed (%d/%d)\n",
		waiting, processing, completed, failed, completed+failed, ps.totalRepos)
	lines++

	for i := lines; i < ps.linesDrawn; i++ {
		_, _ = fmt.Fprintf(ps.writer, "\033[K\n")
	}
	if lines < ps.linesDrawn {
		_, _ = fmt.Fprintf(ps.writer, "\033[%dA", ps.linesDrawn-lines)
	}

	ps.linesDrawn = lines
}

func (ps *ProgressState) renderSimple() {
	waiting, processing, completed, failed := ps.countStatuses()
	_, _ = fmt.Fprintf(ps.writer, "Progress: %d waiting, %d processing, %d done, %d failed (%d/%d)\n",
		waiting, processing, completed, failed, completed+failed, ps.totalRepos)
}

func (ps *ProgressState) countStatuses() (waiting, processing, completed, failed int) {
	for _, status := range ps.statuses {
		switch status.state {
		case statusWaiting:
			waiting++
		case statusProcessing:
			processing++
		case statusCompleted:
			completed++
		case statusFailed:
			failed++
		}
	}
	return
}

func (ps *ProgressState) getStatusIcon(state string) string {
	switch state {
	case statusWaiting:
		return "⏳"
	case statusProcessing:
		return "🔄"
	case statusCompleted:
		return "✅"
	case statusFailed:
		return "❌"
	default:
		return "⏳"
	}
}

func (ps *ProgressState) formatStatus(status repoStatus) string {
	switch status.state {
	case statusWaiting:
		return "waiting"
	case statusProcessing:
		return "processing..."
	case statusCompleted:
		return "completed"
	case statusFailed:
		if status.message != "" {
			return fmt.Sprintf("failed: %s", status.message)
		}
		return "failed"
	default:
		return "unknown"
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func switchBranches(root, target string, workers int, cfg *Config) {
	allRepos, err := findGitRepos(root, cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
	if len(allRepos) == 0 {
		fmt.Println("No repos found")
		return
	}

	repos := cfg.filterReposForExecution(allRepos)
	if len(repos) == 0 {
		fmt.Println("No repos match the specified include/exclude criteria")
		return
	}

	fmt.Printf("Found %d repos (filtered from %d discovered), switching to %s with %d workers...\n", len(repos), len(allRepos), target, workers)

	progress := NewProgressState(repos)
	progress.render()

	repoCh := make(chan RepoInfo, len(repos))
	resCh := make(chan SwitchResult, len(repos))

	var wg sync.WaitGroup

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for r := range repoCh {
				progress.UpdateStatus(r.RelPath, statusProcessing, "")
				res := processSingleRepo(r, target)

				if res.Success {
					progress.UpdateStatus(r.RelPath, statusCompleted, "")
				} else {
					progress.UpdateStatus(r.RelPath, statusFailed, res.Error)
				}

				resCh <- res
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
		remoteCheck := exec.Command("git", "ls-remote", "--exit-code", "--heads", "origin", targetBranch)
		remoteCheck.Dir = repo.Path
		if remoteCheck.Run() == nil {
			out, err := exec.Command("git", "rev-parse", "--is-shallow-repository").Output()
			isShallow := err == nil && strings.TrimSpace(string(out)) == "true"

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
		} else {
			return SwitchResult{RelPath: repo.RelPath, Success: false, Error: "branch not found"}
		}
	}

	switchCmd := exec.Command("git", "switch", targetBranch)
	switchCmd.Dir = repo.Path
	if out, err := switchCmd.CombinedOutput(); err == nil {
		return SwitchResult{RelPath: repo.RelPath, Success: true}
	} else {
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
