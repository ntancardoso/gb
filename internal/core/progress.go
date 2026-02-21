package core

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/eiannone/keyboard"
)

const (
	statusWaiting    = "waiting"
	statusProcessing = "processing"
	statusCompleted  = "completed"
	statusFailed     = "failed"
)

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

type repoStatus struct {
	state   string
	message string
}

type ProgressState struct {
	repos             []RepoInfo
	statuses          []repoStatus
	mu                sync.Mutex
	writeMu           sync.Mutex
	stopOnce          sync.Once
	totalRepos        int
	maxDisplayedRepos int
	writer            io.Writer
	supportsANSI      bool
	linesDrawn        int
	operationName     string
	currentPage       int
	paginationMode    bool
	stopInput         chan struct{}
	stopSpinner       chan struct{}
	spinnerIndex      int
}

func NewProgressState(repos []RepoInfo, operationName string, pageSize int) *ProgressState {
	statuses := make([]repoStatus, len(repos))
	for i := range statuses {
		statuses[i] = repoStatus{state: statusWaiting}
	}

	paginationMode := len(repos) > pageSize

	return &ProgressState{
		repos:             repos,
		statuses:          statuses,
		totalRepos:        len(repos),
		maxDisplayedRepos: pageSize,
		writer:            os.Stdout,
		supportsANSI:      supportsANSI(),
		operationName:     operationName,
		currentPage:       0,
		paginationMode:    paginationMode,
		stopInput:         make(chan struct{}),
		stopSpinner:       make(chan struct{}),
	}
}

func supportsANSI() bool {
	fileInfo, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	if (fileInfo.Mode() & os.ModeCharDevice) == 0 {
		return false
	}

	term := os.Getenv("TERM")
	return term != "dumb"
}

func (ps *ProgressState) UpdateStatus(relPath, status, errorMsg string) {
	ps.mu.Lock()

	repoIndex := ps.findRepoIndex(relPath)
	if repoIndex == -1 {
		ps.mu.Unlock()
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

	ps.renderLocked()
	ps.mu.Unlock()
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
	ps.mu.Lock()
	defer ps.mu.Unlock()
	ps.renderLocked()
}

func (ps *ProgressState) renderLocked() {
	if ps.supportsANSI {
		ps.renderANSI()
	} else {
		ps.renderSimple()
	}
}

func (ps *ProgressState) renderANSI() {
	var buf strings.Builder

	if ps.linesDrawn > 0 {
		buf.WriteString("\r")
		fmt.Fprintf(&buf, "\033[%dA", ps.linesDrawn)
		buf.WriteString("\033[J")
	}

	waiting, processing, completed, failed := ps.countStatuses()

	if completed+failed < ps.totalRepos {
		fmt.Fprintf(&buf, "%s %s...\n", spinnerFrames[ps.spinnerIndex], ps.operationName)
	} else {
		fmt.Fprintf(&buf, "✓ %s - Done\n", ps.operationName)
	}

	var startIdx, endIdx int
	if ps.paginationMode {
		startIdx = ps.currentPage * ps.maxDisplayedRepos
		endIdx = min(startIdx+ps.maxDisplayedRepos, len(ps.repos))
	} else {
		startIdx = 0
		endIdx = min(ps.maxDisplayedRepos, len(ps.repos))
	}

	numWidth := len(fmt.Sprintf("%d", len(ps.repos)))
	for i := startIdx; i < endIdx; i++ {
		icon := ps.getStatusIcon(ps.statuses[i].state)
		statusText := ps.formatStatus(ps.statuses[i])
		fmt.Fprintf(&buf, "[%0*d] %s %s - %s\n", numWidth, i+1, icon, ps.repos[i].RelPath, statusText)
	}

	if ps.paginationMode {
		fmt.Fprintf(&buf, "Page %d/%d (↑/↓ PgUp/PgDn to navigate)\n", ps.currentPage+1, ps.totalPages())
	} else if len(ps.repos) > ps.maxDisplayedRepos {
		fmt.Fprintf(&buf, "... and %d more repos\n", len(ps.repos)-ps.maxDisplayedRepos)
	}

	fmt.Fprintf(&buf, "Status: %d waiting, %d processing, %d done, %d failed (%d/%d)\n",
		waiting, processing, completed, failed, completed+failed, ps.totalRepos)

	lineCount := 1
	lineCount += endIdx - startIdx
	if ps.paginationMode {
		lineCount++
	} else if len(ps.repos) > ps.maxDisplayedRepos {
		lineCount++
	}
	lineCount++

	ps.linesDrawn = lineCount

	ps.writeMu.Lock()
	_, _ = ps.writer.Write([]byte(buf.String()))
	ps.writeMu.Unlock()
}

func (ps *ProgressState) renderSimple() {
	waiting, processing, completed, failed := ps.countStatuses()
	ps.writeMu.Lock()
	_, _ = fmt.Fprintf(ps.writer, "Progress: %d waiting, %d processing, %d done, %d failed (%d/%d)\n",
		waiting, processing, completed, failed, completed+failed, ps.totalRepos)
	ps.writeMu.Unlock()
}

func (ps *ProgressState) RenderFinal() {
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
			msg := strings.ReplaceAll(status.message, "\n", " ")
			msg = strings.ReplaceAll(msg, "\r", "")
			msg = strings.TrimSpace(msg)
			if len(msg) > 80 {
				msg = msg[:77] + "..."
			}
			return fmt.Sprintf("failed: %s", msg)
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

func (ps *ProgressState) totalPages() int {
	if !ps.paginationMode {
		return 1
	}
	return (len(ps.repos) + ps.maxDisplayedRepos - 1) / ps.maxDisplayedRepos
}

func (ps *ProgressState) StartInput() {
	if ps.supportsANSI {
		go ps.runSpinner()
	}

	if !ps.paginationMode || !ps.supportsANSI {
		return
	}

	fileInfo, err := os.Stdin.Stat()
	if err != nil || (fileInfo.Mode()&os.ModeCharDevice) == 0 {
		return
	}

	if err := keyboard.Open(); err != nil {
		ps.paginationMode = false
		if len(ps.repos) > ps.maxDisplayedRepos {
			fmt.Fprintf(os.Stderr, "Note: Keyboard input unavailable, showing first %d repos. Use -ps flag to adjust display size.\n", ps.maxDisplayedRepos)
		}
		return
	}

	go ps.handleInput()
}

func (ps *ProgressState) runSpinner() {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ps.stopSpinner:
			return
		case <-ticker.C:
			ps.mu.Lock()
			_, _, completed, failed := ps.countStatuses()
			if completed+failed < ps.totalRepos {
				ps.spinnerIndex = (ps.spinnerIndex + 1) % len(spinnerFrames)
				ps.renderLocked()
			}
			ps.mu.Unlock()
		}
	}
}

func (ps *ProgressState) StopInput() {
	ps.stopOnce.Do(func() {
		if ps.supportsANSI {
			close(ps.stopSpinner)
		}

		if !ps.paginationMode {
			return
		}

		close(ps.stopInput)
		_ = keyboard.Close()
	})
}

func (ps *ProgressState) handleInput() {
	for {
		select {
		case <-ps.stopInput:
			return
		default:
			_, key, err := keyboard.GetKey()
			if err != nil {
				continue
			}

			ps.mu.Lock()
			changed := false
			switch key {
			case keyboard.KeyArrowDown, keyboard.KeyPgdn:
				if ps.currentPage < ps.totalPages()-1 {
					ps.currentPage++
					changed = true
				}
			case keyboard.KeyArrowUp, keyboard.KeyPgup:
				if ps.currentPage > 0 {
					ps.currentPage--
					changed = true
				}
			}

			if changed {
				ps.renderLocked()
			}
			ps.mu.Unlock()
		}
	}
}
