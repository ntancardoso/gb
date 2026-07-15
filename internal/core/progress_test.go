package core

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
)

func makeTestRepos(n int) []RepoInfo {
	repos := make([]RepoInfo, n)
	for i := range n {
		repos[i] = RepoInfo{RelPath: fmt.Sprintf("repo-%02d", i)}
	}
	return repos
}

func keyMsg(s string) tea.KeyMsg {
	switch s {
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "pgup":
		return tea.KeyMsg{Type: tea.KeyPgUp}
	case "pgdown":
		return tea.KeyMsg{Type: tea.KeyPgDown}
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "ctrl+c":
		return tea.KeyMsg{Type: tea.KeyCtrlC}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	}
}

func isQuit(cmd tea.Cmd) bool {
	if cmd == nil {
		return false
	}
	_, ok := cmd().(tea.QuitMsg)
	return ok
}

func TestModelPromptMsgTransition(t *testing.T) {
	m := newModel(makeTestRepos(3), "op", 10)
	updated, cmd := m.Update(promptMsg{summary: "S"})
	m = updated.(model)
	if !m.done || !m.prompting || m.summary != "S" {
		t.Fatalf("expected done+prompting with summary, got %+v", m)
	}
	if isQuit(cmd) {
		t.Fatal("promptMsg must not quit the program")
	}
}

func TestModelPagingWhilePrompting(t *testing.T) {
	m := newModel(makeTestRepos(25), "op", 10)
	updated, _ := m.Update(promptMsg{summary: "S"})
	m = updated.(model)

	steps := []struct {
		key  string
		page int
	}{
		{"down", 1}, {"pgdown", 2}, {"down", 2},
		{"up", 1}, {"pgup", 0}, {"up", 0},
	}
	for _, s := range steps {
		updated, cmd := m.Update(keyMsg(s.key))
		m = updated.(model)
		if m.page != s.page {
			t.Fatalf("after %q expected page %d, got %d", s.key, s.page, m.page)
		}
		if isQuit(cmd) {
			t.Fatalf("paging key %q must not quit while prompting", s.key)
		}
	}
}

func TestModelPromptAnswers(t *testing.T) {
	cases := []struct {
		key      string
		viewLogs bool
	}{
		{"y", true}, {"Y", true},
		{"n", false}, {"N", false},
		{"enter", false}, {"esc", false}, {"ctrl+c", false},
	}
	for _, c := range cases {
		m := newModel(makeTestRepos(3), "op", 10)
		updated, _ := m.Update(promptMsg{summary: "S"})
		m = updated.(model)
		updated, cmd := m.Update(keyMsg(c.key))
		m = updated.(model)
		if !m.answered || m.viewLogs != c.viewLogs {
			t.Errorf("key %q: expected answered with viewLogs=%v, got answered=%v viewLogs=%v",
				c.key, c.viewLogs, m.answered, m.viewLogs)
		}
		if !isQuit(cmd) {
			t.Errorf("key %q must quit the prompt", c.key)
		}
	}
}

func TestModelPromptIgnoresOtherKeys(t *testing.T) {
	m := newModel(makeTestRepos(3), "op", 10)
	updated, _ := m.Update(promptMsg{summary: "S"})
	m = updated.(model)
	for _, key := range []string{"a", "q", " "} {
		updated, cmd := m.Update(keyMsg(key))
		m = updated.(model)
		if m.answered || isQuit(cmd) {
			t.Errorf("key %q must be ignored while prompting", key)
		}
	}
}

func TestModelCtrlCBeforePrompt(t *testing.T) {
	m := newModel(makeTestRepos(3), "op", 10)
	updated, cmd := m.Update(keyMsg("ctrl+c"))
	m = updated.(model)
	if !isQuit(cmd) {
		t.Fatal("ctrl+c must quit while running")
	}
	if m.answered || m.prompting {
		t.Fatal("ctrl+c before prompt must not mark an answer")
	}
}

func TestModelViewPrompting(t *testing.T) {
	m := newModel(makeTestRepos(3), "op", 10)
	if strings.Contains(m.View(), "View detailed logs?") {
		t.Fatal("prompt must not render before promptMsg")
	}

	updated, _ := m.Update(promptMsg{summary: "SUMMARY-TEXT"})
	m = updated.(model)
	view := m.View()
	if !strings.Contains(view, "SUMMARY-TEXT") || !strings.Contains(view, "View detailed logs? (y/N)") {
		t.Fatalf("expected summary and prompt in view, got:\n%s", view)
	}
	if strings.Contains(view, "to page") {
		t.Fatal("single page must not show the paging hint")
	}

	multi := newModel(makeTestRepos(25), "op", 10)
	updated, _ = multi.Update(promptMsg{summary: "S"})
	multi = updated.(model)
	if !strings.Contains(multi.View(), "to page") {
		t.Fatal("multi-page prompt must show the paging hint")
	}

	updated, _ = m.Update(keyMsg("y"))
	m = updated.(model)
	if !strings.Contains(m.View(), ": y") {
		t.Fatal("answered view must echo the answer")
	}
}

func TestModelTicksIdleWhenDone(t *testing.T) {
	m := newModel(makeTestRepos(3), "op", 10)
	updated, _ := m.Update(promptMsg{summary: "S"})
	m = updated.(model)
	if _, cmd := m.Update(tickMsg{}); cmd != nil {
		t.Fatal("tickMsg must be idle after done")
	}
	if _, cmd := m.Update(spinner.TickMsg{}); cmd != nil {
		t.Fatal("spinner tick must be idle after done")
	}
}
