package output

import (
	"bytes"
	"strings"
	"testing"
)

func TestNew_CreatesWithStdout(t *testing.T) {
	p := New()
	if p == nil {
		t.Fatal("New() returned nil")
	}
}

func TestNewWithWriter_NonTTY(t *testing.T) {
	var buf bytes.Buffer
	p := NewWithWriter(&buf)
	if p == nil {
		t.Fatal("NewWithWriter() returned nil")
	}
	if p.isTTY {
		t.Error("expected isTTY=false for buffer")
	}
}

func TestPrinter_Print(t *testing.T) {
	var buf bytes.Buffer
	p := NewWithWriter(&buf)

	p.Print("hello %s", "world")
	if got := buf.String(); got != "hello world" {
		t.Errorf("Print() = %q, want %q", got, "hello world")
	}
}

func TestPrinter_Println(t *testing.T) {
	var buf bytes.Buffer
	p := NewWithWriter(&buf)

	p.Println("hello")
	if got := buf.String(); !strings.HasSuffix(got, "\n") {
		t.Errorf("Println() should end with newline, got %q", got)
	}
}

func TestPrinter_Info(t *testing.T) {
	var buf bytes.Buffer
	p := NewWithWriter(&buf)

	p.Info("test message", "key", "value")

	got := buf.String()
	if !strings.Contains(got, "INFO") {
		t.Errorf("Info() output should contain INFO, got %q", got)
	}
	if !strings.Contains(got, "test message") {
		t.Errorf("Info() output should contain message, got %q", got)
	}
}

func TestPrinter_Warn(t *testing.T) {
	var buf bytes.Buffer
	p := NewWithWriter(&buf)

	p.Warn("warning message")

	got := buf.String()
	if !strings.Contains(got, "WARN") {
		t.Errorf("Warn() output should contain WARN, got %q", got)
	}
}

func TestPrinter_Error(t *testing.T) {
	var buf bytes.Buffer
	p := NewWithWriter(&buf)

	p.Error("error message")

	got := buf.String()
	// charmbracelet/log uses "ERRO" abbreviation
	if !strings.Contains(got, "ERRO") {
		t.Errorf("Error() output should contain ERRO, got %q", got)
	}
}

func TestPrinter_Debug_DefaultHidden(t *testing.T) {
	var buf bytes.Buffer
	p := NewWithWriter(&buf)

	p.Debug("debug message")

	// Debug is hidden by default
	if buf.Len() > 0 {
		t.Errorf("Debug() should be hidden by default, got %q", buf.String())
	}
}

func TestPrinter_Debug_WhenEnabled(t *testing.T) {
	var buf bytes.Buffer
	p := NewWithWriter(&buf)
	p.SetDebug(true)

	p.Debug("debug message")

	got := buf.String()
	// charmbracelet/log uses "DEBU" abbreviation
	if !strings.Contains(got, "DEBU") {
		t.Errorf("Debug() with SetDebug(true) should contain DEBU, got %q", got)
	}
}

func TestPrinter_Banner_NonTTY(t *testing.T) {
	var buf bytes.Buffer
	p := NewWithWriter(&buf)

	p.Banner("1.2.3")

	got := buf.String()
	if !strings.Contains(got, "gridctl 1.2.3") {
		t.Errorf("Banner() should contain version, got %q", got)
	}
}

func TestPrinter_Section(t *testing.T) {
	var buf bytes.Buffer
	p := NewWithWriter(&buf)

	p.Section("TEST SECTION")

	got := buf.String()
	if !strings.Contains(got, "TEST SECTION") {
		t.Errorf("Section() should contain title, got %q", got)
	}
}
