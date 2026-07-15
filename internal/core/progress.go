package core

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
)

const (
	statusWaiting    = "waiting"
	statusProcessing = "processing"
	statusCompleted  = "completed"
	statusFailed     = "failed"
	statusSkipped    = "skipped"
)

type statusMsg struct{ relPath, state, message string }
type doneMsg struct{}
type promptMsg struct{ summary string }
type tickMsg time.Time

type repoStatus struct {
	state   string
	message string
}

type model struct {
	statuses  map[string]repoStatus
	order     []string
	spinner   spinner.Model
	progBar   progress.Model
	total     int
	pageSize  int
	page      int
	width     int
	startTime time.Time
	done      bool
	opName    string
	prompting bool
	answered  bool
	viewLogs  bool
	summary   string
}

func newModel(repos []RepoInfo, opName string, pageSize int) model {
	s := spinner.New()
	s.Spinner = spinner.Dot

	statuses := make(map[string]repoStatus, len(repos))
	order := make([]string, 0, len(repos))
	for _, r := range repos {
		statuses[r.RelPath] = repoStatus{state: statusWaiting}
		order = append(order, r.RelPath)
	}

	p := progress.New(progress.WithDefaultGradient())

	return model{
		statuses:  statuses,
		order:     order,
		spinner:   s,
		progBar:   p,
		total:     len(repos),
		pageSize:  pageSize,
		width:     80,
		startTime: time.Now(),
		opName:    opName,
	}
}

func tickEvery() tea.Cmd {
	return tea.Every(time.Second, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func (m model) Init() tea.Cmd {
	return tea.Batch(m.spinner.Tick, tickEvery())
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case statusMsg:
		m.statuses[msg.relPath] = repoStatus{state: msg.state, message: msg.message}
		return m, nil

	case doneMsg:
		m.done = true
		return m, tea.Quit

	case promptMsg:
		m.done = true
		m.prompting = true
		m.summary = msg.summary
		return m, nil

	case tea.KeyMsg:
		key := msg.String()
		switch key {
		case "up", "pgup":
			if m.page > 0 {
				m.page--
			}
			return m, nil
		case "down", "pgdown":
			if m.page < m.totalPages()-1 {
				m.page++
			}
			return m, nil
		}
		if m.prompting {
			switch key {
			case "y", "Y":
				m.answered, m.viewLogs = true, true
				return m, tea.Quit
			case "n", "N", "enter", "esc", "ctrl+c":
				m.answered, m.viewLogs = true, false
				return m, tea.Quit
			}
			return m, nil
		}
		if key == "ctrl+c" {
			return m, tea.Quit
		}
		return m, nil

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.progBar.Width = max(msg.Width-30, 20)
		return m, nil

	case spinner.TickMsg:
		if m.done {
			return m, nil
		}
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	case progress.FrameMsg:
		updated, cmd := m.progBar.Update(msg)
		m.progBar = updated.(progress.Model)
		return m, cmd

	case tickMsg:
		if m.done {
			return m, nil
		}
		return m, tickEvery()
	}

	return m, nil
}

func (m model) View() string {
	if m.total == 0 {
		return ""
	}

	var sb strings.Builder

	elapsed := time.Since(m.startTime).Truncate(time.Second)
	if m.done {
		fmt.Fprintf(&sb, "✓ %s - Done\n\n", m.opName)
	} else {
		fmt.Fprintf(&sb, "%s %s  %s\n\n", m.spinner.View(), m.opName, elapsed)
	}

	completed, failed, processing, waiting, skipped := m.countStatuses()
	pct := float64(completed+failed+skipped) / float64(m.total)

	sb.WriteString("  ")
	sb.WriteString(m.progBar.ViewAs(pct))
	fmt.Fprintf(&sb, "  %d/%d  ✅ %d  ❌ %d  ⏭️ %d  🔄 %d  ⏳ %d\n\n",
		completed+failed+skipped, m.total, completed, failed, skipped, processing, waiting)

	for _, relPath := range m.sortedPage() {
		sb.WriteString("  ")
		sb.WriteString(m.formatRepoLine(relPath, m.statuses[relPath]))
		sb.WriteString("\n")
	}

	if m.totalPages() > 1 {
		fmt.Fprintf(&sb, "\n  Page %d/%d  ↑↓ PgUp/PgDn\n", m.page+1, m.totalPages())
	}

	if m.prompting {
		sb.WriteString("\n" + m.summary + "\n\n")
		prompt := "View detailed logs? (y/N)"
		switch {
		case m.answered && m.viewLogs:
			prompt += ": y"
		case m.answered:
			prompt += ": n"
		case m.totalPages() > 1:
			prompt += "  " + StyleDim.Render("↑↓ PgUp/PgDn to page")
		}
		sb.WriteString(prompt + "\n")
	}

	return sb.String()
}

func (m model) formatRepoLine(relPath string, st repoStatus) string {
	switch st.state {
	case statusFailed:
		errSuffix := ""
		if st.message != "" {
			msg := strings.ReplaceAll(st.message, "\n", " ")
			msg = strings.TrimSpace(msg)
			runes := []rune(msg)
			if len(runes) > 60 {
				msg = string(runes[:57]) + "..."
			}
			errSuffix = "  " + StyleErrInline.Render(msg)
		}
		return "❌ " + StyleFailed.Render(relPath) + errSuffix
	case statusProcessing:
		return "🔄 " + StyleProcessing.Render(relPath)
	case statusWaiting:
		return "⏳ " + StyleWaiting.Render(relPath)
	case statusCompleted:
		return "✅ " + StyleSuccess.Render(relPath)
	case statusSkipped:
		skipSuffix := ""
		if st.message != "" {
			skipSuffix = "  " + StyleDim.Render(st.message)
		}
		return "⏭️ " + StyleSkipped.Render(relPath) + skipSuffix
	default:
		return "⏳ " + relPath
	}
}

func (m model) countStatuses() (completed, failed, processing, waiting, skipped int) {
	for _, st := range m.statuses {
		switch st.state {
		case statusCompleted:
			completed++
		case statusFailed:
			failed++
		case statusProcessing:
			processing++
		case statusWaiting:
			waiting++
		case statusSkipped:
			skipped++
		}
	}
	return
}

func (m model) totalPages() int {
	if m.pageSize <= 0 {
		return 1
	}
	return (len(m.order) + m.pageSize - 1) / m.pageSize
}

func (m model) sortedPage() []string {
	start := m.page * m.pageSize
	end := min(start+m.pageSize, len(m.order))
	pageItems := m.order[start:end]

	priority := map[string]int{
		statusFailed:     0,
		statusProcessing: 1,
		statusWaiting:    2,
		statusSkipped:    3,
		statusCompleted:  4,
	}

	sorted := make([]string, len(pageItems))
	copy(sorted, pageItems)
	for i := 1; i < len(sorted); i++ {
		for j := i; j > 0 && priority[m.statuses[sorted[j]].state] < priority[m.statuses[sorted[j-1]].state]; j-- {
			sorted[j], sorted[j-1] = sorted[j-1], sorted[j]
		}
	}
	return sorted
}

type ProgressState struct {
	program      *tea.Program
	supportsANSI bool
	stopped      atomic.Bool
	wg           sync.WaitGroup
	stopOnce     sync.Once
	finalModel   tea.Model // written only by the StartInput goroutine; read after wg.Wait
	runErr       error
}

func NewProgressState(repos []RepoInfo, operationName string, pageSize int) *ProgressState {
	ansi := supportsANSI()
	ps := &ProgressState{supportsANSI: ansi}
	if ansi {
		m := newModel(repos, operationName, pageSize)
		ps.program = tea.NewProgram(m)
	}
	return ps
}

func supportsANSI() bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	fi, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	if (fi.Mode() & os.ModeCharDevice) == 0 {
		return false
	}
	return os.Getenv("TERM") != "dumb"
}

func (ps *ProgressState) StartInput() {
	if !ps.supportsANSI || ps.program == nil {
		return
	}
	ps.wg.Go(func() {
		m, err := ps.program.Run()
		ps.finalModel, ps.runErr = m, err
		if err != nil {
			fmt.Fprintf(os.Stderr, "TUI error: %v\n", err)
		}
	})
}

func (ps *ProgressState) StopInput() {
	ps.stopOnce.Do(func() {
		ps.stopped.Store(true)
		if ps.program != nil {
			ps.program.Send(doneMsg{})
		}
		ps.wg.Wait()
	})
}

func (ps *ProgressState) UpdateStatus(relPath, status, errorMsg string) {
	if ps.stopped.Load() {
		return
	}
	if ps.program != nil {
		ps.program.Send(statusMsg{relPath: relPath, state: status, message: errorMsg})
		return
	}
	fmt.Printf("%s: %s\n", relPath, status)
}

// FinishAndPromptViewLogs stops progress, shows the summary, and asks whether
// to view detailed logs. In TUI mode the prompt runs inside the still-running
// bubbletea program so the paging keys stay live; otherwise it falls back to a
// plain summary print plus PromptViewLogs. Shares stopOnce with StopInput.
func (ps *ProgressState) FinishAndPromptViewLogs(summary string) bool {
	viewLogs, ran := false, false
	ps.stopOnce.Do(func() {
		ran = true
		ps.stopped.Store(true)
		if ps.program == nil || !stdinIsCharDevice() {
			if ps.program != nil {
				ps.program.Send(doneMsg{})
			}
			ps.wg.Wait()
			fmt.Println("\n" + summary)
			viewLogs = PromptViewLogs()
			return
		}
		ps.program.Send(promptMsg{summary: summary})
		ps.wg.Wait()
		if m, ok := ps.finalModel.(model); ok && ps.runErr == nil && m.prompting {
			viewLogs = m.viewLogs
			return
		}
		// TUI exited before the prompt rendered (ctrl+c mid-run, Run error,
		// external SIGINT): recover exactly as the non-TUI path does.
		fmt.Println("\n" + summary)
		viewLogs = PromptViewLogs()
	})
	if !ran {
		fmt.Println("\n" + summary)
		return PromptViewLogs()
	}
	return viewLogs
}

func progressStatusFromErr(err error) (string, string) {
	if err == nil {
		return statusCompleted, ""
	}
	msg := strings.ReplaceAll(err.Error(), "\n", " ")
	if r := []rune(msg); len(r) > 50 {
		msg = string(r[:47]) + "..."
	}
	return statusFailed, msg
}
