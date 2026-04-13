package core

import (
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

func init() {
	if !supportsANSI() {
		lipgloss.SetColorProfile(termenv.Ascii)
	}
}

var (
	StyleFailed     = lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Bold(true)
	StyleSuccess    = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	StyleProcessing = lipgloss.NewStyle().Foreground(lipgloss.Color("14"))
	StyleSkipped    = lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
	StyleDim        = lipgloss.NewStyle().Faint(true)
	StyleErrInline  = lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Faint(true)
	StyleBold       = lipgloss.NewStyle().Bold(true)

	StyleWaiting = StyleDim
	StyleInfo    = StyleProcessing
)
