package core

import (
	"fmt"
	"io"
	"os"
	"sync"
)

const (
	maxDisplayedRepos = 30
	statusWaiting     = "waiting"
	statusProcessing  = "processing"
	statusCompleted   = "completed"
	statusFailed      = "failed"
)

type repoStatus struct {
	state   string
	message string
}

type ProgressState struct {
	repos         []RepoInfo
	statuses      []repoStatus
	mu            sync.Mutex
	totalRepos    int
	writer        io.Writer
	supportsANSI  bool
	linesDrawn    int
	operationName string
}

func NewProgressState(repos []RepoInfo, operationName string) *ProgressState {
	statuses := make([]repoStatus, len(repos))
	for i := range statuses {
		statuses[i] = repoStatus{state: statusWaiting}
	}

	return &ProgressState{
		repos:         repos,
		statuses:      statuses,
		totalRepos:    len(repos),
		writer:        os.Stdout,
		supportsANSI:  supportsANSI(),
		operationName: operationName,
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

		fmt.Fprintf(ps.writer, "\033[%dA", ps.linesDrawn)
	}

	lines := 0
	waiting, processing, completed, failed := ps.countStatuses()


	fmt.Fprintf(ps.writer, "\r\033[KProgress: %s...\n", ps.operationName)
	lines++

	displayCount := min(maxDisplayedRepos, len(ps.repos))
	for i := 0; i < displayCount; i++ {
		icon := ps.getStatusIcon(ps.statuses[i].state)
		statusText := ps.formatStatus(ps.statuses[i])
		fmt.Fprintf(ps.writer, "\r\033[K[%d] %s %s - %s\n", i+1, icon, ps.repos[i].RelPath, statusText)
		lines++
	}

	if len(ps.repos) > maxDisplayedRepos {
		fmt.Fprintf(ps.writer, "\r\033[K... and %d more repos\n", len(ps.repos)-maxDisplayedRepos)
		lines++
	}

	fmt.Fprintf(ps.writer, "\r\033[KStatus: %d waiting, %d processing, %d done, %d failed (%d/%d)\n",
		waiting, processing, completed, failed, completed+failed, ps.totalRepos)
	lines++


	for i := lines; i < ps.linesDrawn; i++ {
		fmt.Fprintf(ps.writer, "\r\033[K\n")
	}

	// Move cursor back up if we cleared extra lines
	if lines < ps.linesDrawn {
		fmt.Fprintf(ps.writer, "\033[%dA", ps.linesDrawn-lines)
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
