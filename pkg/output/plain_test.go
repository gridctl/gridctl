package output

import (
	"bytes"
	"strings"
	"testing"

	"github.com/jedib0t/go-pretty/v6/table"
)

// boxRunes are the rounded-style glyphs that must never appear in plain
// (or piped) table output.
const boxRunes = "╭╮╰╯│─┬┴├┤┼"

func containsBoxRune(s string) bool {
	return strings.ContainsAny(s, boxRunes)
}

func TestNewTableWriterPlainHasNoBoxRunes(t *testing.T) {
	var buf bytes.Buffer
	w := NewTableWriter(&buf, true)
	w.AppendHeader(table.Row{"Name", "State"})
	w.AppendRow(table.Row{"alpha", "running"})
	w.AppendRow(table.Row{"beta", "stopped"})
	w.Render()

	out := buf.String()
	if containsBoxRune(out) {
		t.Errorf("plain table contains box-drawing runes:\n%s", out)
	}
	if !strings.Contains(out, "NAME") || !strings.Contains(out, "STATE") {
		t.Errorf("plain table dropped headers:\n%s", out)
	}
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 3 {
		t.Errorf("plain table should be one line per record (header + 2 rows), got %d lines:\n%s", len(lines), out)
	}
	// Columns must be separated by at least two spaces for awk/cut users.
	if !strings.Contains(lines[1], "alpha  ") {
		t.Errorf("plain table columns not separated by 2+ spaces:\n%s", out)
	}
}

func TestNewTableWriterAutoPlainOnNonTTY(t *testing.T) {
	// A bytes.Buffer is not a terminal, so plain rendering applies even
	// without the explicit flag.
	var buf bytes.Buffer
	w := NewTableWriter(&buf, false)
	w.AppendHeader(table.Row{"Key", "Value"})
	w.AppendRow(table.Row{"k", "v"})
	w.Render()

	if containsBoxRune(buf.String()) {
		t.Errorf("non-TTY table output contains box-drawing runes:\n%s", buf.String())
	}
}

func TestPrinterTablesAutoPlainOnNonTTY(t *testing.T) {
	var buf bytes.Buffer
	p := NewWithWriter(&buf)
	p.Summary([]WorkloadSummary{{Name: "srv", Type: "mcp-server", Transport: "http", State: "running"}})
	p.Gateways([]GatewaySummary{{Name: "stack", Port: 8180, PID: 42, Status: "running", Started: "1 minute ago"}})

	out := buf.String()
	if containsBoxRune(out) {
		t.Errorf("Printer table output on a non-TTY writer contains box runes:\n%s", out)
	}
	if strings.Contains(out, "\033") {
		t.Errorf("Printer plain output contains ANSI escapes:\n%s", out)
	}
	for _, want := range []string{"srv", "running", "GATEWAYS", "8180"} {
		if !strings.Contains(out, want) {
			t.Errorf("plain Printer output missing %q:\n%s", want, out)
		}
	}
}

func TestPlainTableStyleDecisionMatrix(t *testing.T) {
	// Mirrors color_test.go: the style decision depends only on the plain
	// flag and TTY-ness of the writer.
	tests := []struct {
		name      string
		plain     bool
		wantPlain bool
	}{
		{"non-TTY default", false, true},
		{"non-TTY forced plain", true, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			p := NewWithWriter(&buf)
			p.SetPlain(tt.plain)
			style := p.tableStyle()
			gotPlain := style.Name == "gridctl-plain"
			if gotPlain != tt.wantPlain {
				t.Errorf("tableStyle() plain = %v, want %v", gotPlain, tt.wantPlain)
			}
		})
	}
}
