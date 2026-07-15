package output

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// spinnerFrames animate a running phase on interactive terminals. The
// static line path (non-TTY, CI, accessible consumers) never sees them:
// speech synthesis mangles braille glyphs, so plain text is the base
// behavior and the animation is chrome on top.
var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

const spinnerInterval = 120 * time.Millisecond

// Reporter prints apply-phase progress. The base behavior is a static
// "<name>..." line on start and a "<name>... done" line on completion;
// when animate is enabled a spinner redraws the line in place instead.
// A nil *Reporter is a no-op so quiet mode can skip construction.
type Reporter struct {
	w       io.Writer
	animate bool
	color   bool

	mu      sync.Mutex
	phase   string
	started time.Time
	stop    chan struct{}
	done    chan struct{}
}

// NewReporter creates a phase reporter writing to w. Animation runs only
// when w is an interactive terminal, the CI environment variable is
// unset, styling is not globally disabled (NO_COLOR, TERM=dumb,
// --no-color), and ACCESSIBLE is not requested; everything else gets
// static lines.
func NewReporter(w io.Writer) *Reporter {
	return &Reporter{
		w: w,
		animate: isTerminal(w) &&
			os.Getenv("CI") == "" &&
			os.Getenv("ACCESSIBLE") == "" &&
			colorAllowedByEnv(),
		color: ColorEnabled(w),
	}
}

// StartPhase begins a named phase. Any phase still running is finished
// successfully first, so call sites never have to pair Start/End manually
// across seams.
//
// Pass spin=false for phases whose window carries foreign output (the
// docker pull/build slog stream): the phase then renders as static lines
// even on a TTY, so redraw frames never interleave with real output.
func (r *Reporter) StartPhase(name string, spin bool) {
	if r == nil {
		return
	}
	r.EndPhase(true)

	r.mu.Lock()
	defer r.mu.Unlock()
	r.phase = name
	r.started = time.Now()

	if !r.animate || !spin {
		fmt.Fprintf(r.w, "%s...\n", name)
		return
	}

	r.stop = make(chan struct{})
	r.done = make(chan struct{})
	go r.spin(name, r.stop, r.done)
}

// spin redraws the phase line until stopped. Frames are throttled by
// spinnerInterval; the line is cleared before the goroutine exits so the
// final status line never collides with a stale frame.
func (r *Reporter) spin(name string, stop <-chan struct{}, done chan<- struct{}) {
	defer close(done)
	ticker := time.NewTicker(spinnerInterval)
	defer ticker.Stop()

	frame := 0
	for {
		select {
		case <-stop:
			r.clearLine(name)
			return
		case <-ticker.C:
			glyph := spinnerFrames[frame%len(spinnerFrames)]
			if r.color {
				glyph = lipgloss.NewStyle().Foreground(ColorAmber).Render(glyph)
			}
			fmt.Fprintf(r.w, "\r%s %s...", glyph, name)
			frame++
		}
	}
}

// clearLine erases the in-place spinner line with carriage returns and
// spaces only (no ANSI escapes), so NO_COLOR output stays clean.
func (r *Reporter) clearLine(name string) {
	fmt.Fprintf(r.w, "\r%s\r", strings.Repeat(" ", len(name)+6))
}

// EndPhase completes the running phase, printing its terminal status
// line. Safe to call when no phase is active.
func (r *Reporter) EndPhase(ok bool) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.phase == "" {
		return
	}
	name := r.phase
	elapsed := time.Since(r.started).Round(100 * time.Millisecond)
	r.phase = ""

	if r.stop != nil {
		close(r.stop)
		<-r.done
		r.stop = nil
		r.done = nil
	}

	if !r.animate {
		if ok {
			fmt.Fprintf(r.w, "%s... done\n", name)
		} else {
			fmt.Fprintf(r.w, "%s... failed\n", name)
		}
		return
	}

	mark, label := "✓", fmt.Sprintf("%s... done (%s)", name, elapsed)
	if !ok {
		mark, label = "✗", fmt.Sprintf("%s... failed", name)
	}
	if r.color {
		style := lipgloss.NewStyle().Foreground(ColorGreen)
		if !ok {
			style = lipgloss.NewStyle().Foreground(ColorRed)
		}
		mark = style.Render(mark)
	}
	fmt.Fprintf(r.w, "%s %s\n", mark, label)
}
