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
)

type statusMsg struct{ relPath, state, message string }
type doneMsg struct{}
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

	case tea.KeyMsg:
		switch msg.String() {
		case "up", "pgup":
			if m.page > 0 {
				m.page--
			}
		case "down", "pgdown":
			if m.page < m.totalPages()-1 {
				m.page++
			}
		case "ctrl+c":
			return m, tea.Quit
		}
		return m, nil

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.progBar.Width = msg.Width - 30
		if m.progBar.Width < 20 {
			m.progBar.Width = 20
		}
		return m, nil

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	case progress.FrameMsg:
		updated, cmd := m.progBar.Update(msg)
		m.progBar = updated.(progress.Model)
		return m, cmd

	case tickMsg:
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
		sb.WriteString(fmt.Sprintf("✓ %s - Done\n\n", m.opName))
	} else {
		sb.WriteString(fmt.Sprintf("%s %s  %s\n\n", m.spinner.View(), m.opName, elapsed))
	}

	completed, failed, processing, waiting := m.countStatuses()
	pct := float64(completed+failed) / float64(m.total)

	sb.WriteString("  ")
	sb.WriteString(m.progBar.ViewAs(pct))
	sb.WriteString(fmt.Sprintf("  %d/%d  ✅ %d  ❌ %d  🔄 %d  ⏳ %d\n\n",
		completed+failed, m.total, completed, failed, processing, waiting))

	for _, relPath := range m.sortedPage() {
		sb.WriteString("  ")
		sb.WriteString(m.formatRepoLine(relPath, m.statuses[relPath]))
		sb.WriteString("\n")
	}

	if m.totalPages() > 1 {
		sb.WriteString(fmt.Sprintf("\n  Page %d/%d  ↑↓ PgUp/PgDn\n", m.page+1, m.totalPages()))
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
	default:
		return "⏳ " + relPath
	}
}

func (m model) countStatuses() (completed, failed, processing, waiting int) {
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
		statusCompleted:  3,
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
	ps.wg.Add(1)
	go func() {
		defer ps.wg.Done()
		if _, err := ps.program.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "TUI error: %v\n", err)
		}
	}()
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

func (ps *ProgressState) render()      {}
func (ps *ProgressState) RenderFinal() {}
