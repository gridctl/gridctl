package output

import (
	"bytes"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestIsTerminalOnNonFileWriter(t *testing.T) {
	if IsTerminal(&bytes.Buffer{}) {
		t.Error("a bytes.Buffer must not report as a terminal")
	}
}

func TestNewReporterNeverAnimatesWhenStyleDisabled(t *testing.T) {
	// Buffers are non-TTY so animate is false regardless, but the env
	// gates must also hold: with NO_COLOR set the constructor must not
	// enable animation even if the writer were a terminal. Exercise the
	// decision inputs via colorAllowedByEnv, mirroring color_test.go.
	t.Setenv("NO_COLOR", "1")
	r := NewReporter(&bytes.Buffer{})
	if r.animate {
		t.Error("reporter must not animate when NO_COLOR is set")
	}
	if colorAllowedByEnv() {
		t.Error("colorAllowedByEnv must be false with NO_COLOR set")
	}
}

func TestNilReporterIsNoOp(t *testing.T) {
	var r *Reporter
	r.StartPhase("anything", true)
	r.EndPhase(true)
	r.EndPhase(false)
}

func TestReporterStaticLines(t *testing.T) {
	var buf bytes.Buffer
	r := NewReporter(&buf) // buffer is not a TTY, so animation is off

	r.StartPhase("Pulling images", false)
	r.EndPhase(true)
	r.StartPhase("Starting gateway", true) // spin requested, but non-TTY stays static
	r.EndPhase(false)

	out := buf.String()
	wantLines := []string{
		"Pulling images...",
		"Pulling images... done",
		"Starting gateway...",
		"Starting gateway... failed",
	}
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != len(wantLines) {
		t.Fatalf("expected %d lines, got %d:\n%s", len(wantLines), len(lines), out)
	}
	for i, want := range wantLines {
		if lines[i] != want {
			t.Errorf("line %d = %q, want %q", i, lines[i], want)
		}
	}
	if strings.Contains(out, "\033") || strings.Contains(out, "\r") {
		t.Errorf("static reporter output must carry no ANSI or carriage returns:\n%q", out)
	}
}

func TestReporterEndWithoutStartIsSafe(t *testing.T) {
	var buf bytes.Buffer
	r := NewReporter(&buf)
	r.EndPhase(true)
	if buf.Len() != 0 {
		t.Errorf("EndPhase without a phase should print nothing, got %q", buf.String())
	}
}

func TestReporterStartClosesPriorPhase(t *testing.T) {
	var buf bytes.Buffer
	r := NewReporter(&buf)
	r.StartPhase("one", false)
	r.StartPhase("two", false)
	r.EndPhase(true)

	out := buf.String()
	if !strings.Contains(out, "one... done") {
		t.Errorf("starting a new phase should complete the prior one:\n%s", out)
	}
}

// syncBuffer guards writes because the animated spinner writes from a
// goroutine.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.String()
}

func TestReporterAnimatedSpinnerStopsBeforeStatusLine(t *testing.T) {
	buf := &syncBuffer{}
	// Construct directly: buffers are never terminals, so this is the only
	// way to exercise the animation path deterministically.
	r := &Reporter{w: buf, animate: true}

	r.StartPhase("Starting gateway", true)
	time.Sleep(3 * spinnerInterval)
	r.EndPhase(true)

	out := buf.String()
	if !strings.Contains(out, "Starting gateway... done") {
		t.Errorf("animated phase should end with a done line:\n%q", out)
	}
	// The final line must not carry a stale frame: after the last \r the
	// text is the status line, not a spinner glyph.
	tail := out[strings.LastIndex(out, "\r")+1:]
	if !strings.HasPrefix(tail, "✓") && !strings.HasPrefix(strings.TrimSpace(tail), "✓") {
		t.Errorf("spinner left a stale frame before the status line: %q", tail)
	}
	if strings.Contains(out, "\033") {
		t.Errorf("spinner without color must not emit ANSI escapes:\n%q", out)
	}
}

func TestReporterAnimatedStaticPhaseNeverSpins(t *testing.T) {
	buf := &syncBuffer{}
	r := &Reporter{w: buf, animate: true}

	r.StartPhase("Pulling images", false)
	time.Sleep(2 * spinnerInterval)
	r.EndPhase(true)

	out := buf.String()
	if strings.Contains(out, "\r") {
		t.Errorf("spin=false phase must not redraw even on a TTY reporter:\n%q", out)
	}
	if !strings.Contains(out, "Pulling images...") {
		t.Errorf("static phase line missing:\n%q", out)
	}
}
