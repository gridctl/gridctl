package output

import (
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/log"
)

// Amber color theme based on Gridctl web UI design system.
// Primary amber (#f59e0b) for key elements.
var (
	ColorAmber = lipgloss.Color("#f59e0b") // Primary brand color
	ColorWhite = lipgloss.Color("#fafaf9") // text-text-primary
	ColorMuted = lipgloss.Color("#78716c") // text-text-muted
	ColorGreen = lipgloss.Color("#10b981") // status-running
	ColorRed   = lipgloss.Color("#f43f5e") // status-error
	ColorGray  = lipgloss.Color("#a8a29e") // text-text-secondary
)

// amberStyles returns charmbracelet/log styles with amber theme.
func amberStyles() *log.Styles {
	styles := log.DefaultStyles()

	// Level styling with amber accent
	styles.Levels[log.InfoLevel] = lipgloss.NewStyle().
		SetString("INFO").
		Foreground(ColorAmber).
		Bold(true)

	styles.Levels[log.WarnLevel] = lipgloss.NewStyle().
		SetString("WARN").
		Foreground(lipgloss.Color("#eab308")). // Yellow-amber
		Bold(true)

	styles.Levels[log.ErrorLevel] = lipgloss.NewStyle().
		SetString("ERROR").
		Foreground(ColorRed).
		Bold(true)

	styles.Levels[log.DebugLevel] = lipgloss.NewStyle().
		SetString("DEBUG").
		Foreground(ColorMuted)

	// Timestamp in muted gray
	styles.Timestamp = lipgloss.NewStyle().
		Foreground(ColorMuted)

	// Keys in amber for structured logging
	styles.Key = lipgloss.NewStyle().
		Foreground(ColorAmber)

	// Values in neutral gray
	styles.Value = lipgloss.NewStyle().
		Foreground(ColorGray)

	return styles
}
