// Package output provides terminal output formatting for gridctl with amber color theme.
package output

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/log"
	"github.com/mattn/go-isatty"
)

// defaultLevel is the minimum log level applied to loggers created by New
// and NewWithWriter. Wired to the global --log-level flag.
var defaultLevel = log.InfoLevel

// SetDefaultLevel sets the minimum level for subsequently created Printers.
// charmbracelet/log levels share slog's numeric values, so the conversion
// is direct.
func SetDefaultLevel(l slog.Level) {
	defaultLevel = log.Level(l)
}

// Printer handles terminal output with amber-themed styling.
type Printer struct {
	out    io.Writer
	logger *log.Logger
	isTTY  bool
	color  bool
	plain  bool
}

// New creates a Printer writing to stdout with amber theme.
func New() *Printer {
	return NewWithWriter(os.Stdout)
}

// NewWithWriter creates a Printer with a custom writer.
func NewWithWriter(w io.Writer) *Printer {
	isTTY := isTerminal(w)
	color := ColorEnabled(w)

	logger := log.NewWithOptions(w, log.Options{
		ReportTimestamp: true,
		TimeFormat:      time.TimeOnly, // HH:MM:SS
		Level:           defaultLevel,
	})

	if color {
		logger.SetStyles(amberStyles())
	}

	return &Printer{
		out:    w,
		logger: logger,
		isTTY:  isTTY,
		color:  color,
	}
}

// isTerminal checks if the writer is a TTY (for color support).
func isTerminal(w io.Writer) bool {
	if f, ok := w.(*os.File); ok {
		return isatty.IsTerminal(f.Fd()) || isatty.IsCygwinTerminal(f.Fd())
	}
	return false
}

// IsTerminal reports whether w is an interactive terminal. Callers use it
// to gate interactive chrome (spinners, prompts) that must never run
// against pipes or CI logs.
func IsTerminal(w io.Writer) bool {
	return isTerminal(w)
}

// SetPlain forces grep-friendly table rendering (no box-drawing) even when
// the writer is a terminal. Wired to per-command --plain flags.
func (p *Printer) SetPlain(plain bool) {
	p.plain = plain
}

// Info logs an info message with optional key-value pairs.
func (p *Printer) Info(msg string, keyvals ...any) {
	p.logger.Info(msg, keyvals...)
}

// Warn logs a warning message with optional key-value pairs.
func (p *Printer) Warn(msg string, keyvals ...any) {
	p.logger.Warn(msg, keyvals...)
}

// Error logs an error message with optional key-value pairs.
func (p *Printer) Error(msg string, keyvals ...any) {
	p.logger.Error(msg, keyvals...)
}

// Debug logs a debug message with optional key-value pairs.
func (p *Printer) Debug(msg string, keyvals ...any) {
	p.logger.Debug(msg, keyvals...)
}

// SetDebug enables debug-level logging.
func (p *Printer) SetDebug(enabled bool) {
	if enabled {
		p.logger.SetLevel(log.DebugLevel)
	} else {
		p.logger.SetLevel(log.InfoLevel)
	}
}

// SetLevel sets the printer's minimum log level from an slog level.
func (p *Printer) SetLevel(l slog.Level) {
	p.logger.SetLevel(log.Level(l))
}

// Banner prints the ASCII logo with version information.
func (p *Printer) Banner(ver string) {
	if !p.isTTY {
		fmt.Fprintf(p.out, "gridctl %s\n\n", ver)
		return
	}

	// Colors (no-ops when color is disabled, e.g. NO_COLOR or --no-color)
	amber := lipgloss.NewStyle()
	white := lipgloss.NewStyle()
	muted := lipgloss.NewStyle()
	if p.color {
		amber = amber.Foreground(ColorAmber)
		white = white.Foreground(ColorWhite)
		muted = muted.Foreground(ColorMuted)
	}

	// "grid" part in amber
	gridPart := []string{
		`            _     _`,
		`           (_)   | |`,
		`  __ _ _ __ _  __| |`,
		" / _` | '__| |/ _` |",
		`| (_| | |  | | (_| |`,
		` \__, |_|  |_|\__,_|`,
		`  __/ |`,
		` |___/`,
	}

	// "ctl" part in white
	ctlPart := []string{
		`      _   _`,
		`    | | | |`,
		` ___| |_| |`,
		`/ __| __| |`,
		` (__| |_| |`,
		`\___|\__|_|`,
		``,
		``,
	}

	for i := 0; i < len(gridPart); i++ {
		fmt.Fprint(p.out, amber.Render(gridPart[i]))
		if i < len(ctlPart) && ctlPart[i] != "" {
			fmt.Fprint(p.out, white.Render(ctlPart[i]))
		}
		fmt.Fprintln(p.out)
	}

	// Version line
	fmt.Fprintf(p.out, "\n  %s %s\n\n", muted.Render("version"), amber.Render(ver))
}

// Hint prints a short next-step suggestion. Hints are conversational
// chrome: they are suppressed when the writer is not a terminal so
// scripts, pipes, and JSON consumers never see them.
func (p *Printer) Hint(format string, args ...any) {
	if !p.isTTY {
		return
	}
	msg := fmt.Sprintf(format, args...)
	if p.color {
		msg = lipgloss.NewStyle().Foreground(ColorMuted).Render(msg)
	}
	fmt.Fprintf(p.out, "\n%s\n", msg)
}

// Print writes a message directly to output without formatting.
func (p *Printer) Print(format string, args ...any) {
	fmt.Fprintf(p.out, format, args...)
}

// Println writes a message with newline directly to output.
func (p *Printer) Println(args ...any) {
	fmt.Fprintln(p.out, args...)
}
