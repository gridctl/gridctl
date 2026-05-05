package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	ossignal "os/signal"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/gridctl/gridctl/pkg/state"
	"github.com/gridctl/gridctl/pkg/telemetry"

	"github.com/fsnotify/fsnotify"
	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/spf13/cobra"
)

var (
	telemetryStatusJSON bool
	telemetryWipeServer string
	telemetryWipeSignal string
	telemetryWipeYes    bool
	telemetryTailSignal string
)

var telemetryCmd = &cobra.Command{
	Use:   "telemetry",
	Short: "Inspect and manage persisted telemetry",
	Long: `Inspect and manage opt-in telemetry persistence under ~/.gridctl/telemetry/.

These commands operate directly on the on-disk files and do not require a
running daemon. Persistence itself is configured per-stack and per-server in
the stack YAML.`,
}

var telemetryStatusCmd = &cobra.Command{
	Use:   "status [stack]",
	Short: "Show persisted telemetry inventory",
	Long: `Lists the on-disk footprint of persisted telemetry.

With no argument, walks every stack under ~/.gridctl/telemetry/. Pass a stack
name to scope the report. Use --json for machine-readable output.`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		stack := ""
		if len(args) == 1 {
			stack = args[0]
		}
		return runTelemetryStatus(stack, telemetryStatusJSON)
	},
}

var telemetryWipeCmd = &cobra.Command{
	Use:   "wipe [stack]",
	Short: "Delete persisted telemetry files",
	Long: `Deletes persisted telemetry files. Without a stack argument or any flags,
removes everything under ~/.gridctl/telemetry/.

--server scopes the wipe to a single MCP server.
--signal scopes the wipe to one signal type (logs, metrics, traces).
-y / --yes skips the confirmation prompt.`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		stack := ""
		if len(args) == 1 {
			stack = args[0]
		}
		return runTelemetryWipe(stack, telemetryWipeServer, telemetryWipeSignal, telemetryWipeYes)
	},
}

var telemetryTailCmd = &cobra.Command{
	Use:   "tail <stack> <server>",
	Short: "Follow a persisted telemetry file (tail -f)",
	Long: `Follows the active <signal>.jsonl file for the given stack and server,
printing new NDJSON lines as they're written. Lumberjack rotations are
detected automatically. Press Ctrl-C to exit.`,
	Args: cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runTelemetryTail(args[0], args[1], telemetryTailSignal)
	},
}

func init() {
	telemetryStatusCmd.Flags().BoolVar(&telemetryStatusJSON, "json", false, "Output as JSON")

	telemetryWipeCmd.Flags().StringVar(&telemetryWipeServer, "server", "", "Limit to a single MCP server")
	telemetryWipeCmd.Flags().StringVar(&telemetryWipeSignal, "signal", "", "Limit to a single signal (logs, metrics, traces)")
	telemetryWipeCmd.Flags().BoolVarP(&telemetryWipeYes, "yes", "y", false, "Skip confirmation prompt")

	telemetryTailCmd.Flags().StringVar(&telemetryTailSignal, "signal", "", "Signal to follow (logs, metrics, traces)")
	_ = telemetryTailCmd.MarkFlagRequired("signal")

	telemetryCmd.AddCommand(telemetryStatusCmd)
	telemetryCmd.AddCommand(telemetryWipeCmd)
	telemetryCmd.AddCommand(telemetryTailCmd)
	rootCmd.AddCommand(telemetryCmd)
}

// stackInventory pairs a stack name with its inventory records so the CLI can
// render a multi-stack report without losing the parent stack on each row.
type stackInventory struct {
	Stack   string                       `json:"stack"`
	Records []telemetry.InventoryRecord  `json:"records"`
}

// statusRow is the flat per-(stack, signal) row used for tables and JSON
// output. Times are RFC3339 strings so the JSON form is consumable by jq
// without a custom time decoder.
type statusRow struct {
	Stack     string `json:"stack"`
	Server    string `json:"server"`
	Signal    string `json:"signal"`
	Path      string `json:"path"`
	SizeBytes int64  `json:"sizeBytes"`
	FileCount int    `json:"fileCount"`
	Oldest    string `json:"oldest,omitempty"`
	Newest    string `json:"newest,omitempty"`
}

// listTelemetryStacks returns the names of stacks that have a directory under
// ~/.gridctl/telemetry/. A missing root returns an empty slice without error.
func listTelemetryStacks() ([]string, error) {
	root := state.TelemetryDir()
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read telemetry root: %w", err)
	}
	stacks := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			stacks = append(stacks, e.Name())
		}
	}
	sort.Strings(stacks)
	return stacks, nil
}

// gatherInventories returns per-stack inventory records. When stack is empty,
// every stack under TelemetryDir is walked.
func gatherInventories(stack string) ([]stackInventory, error) {
	stacks := []string{stack}
	if stack == "" {
		all, err := listTelemetryStacks()
		if err != nil {
			return nil, err
		}
		stacks = all
	}
	out := make([]stackInventory, 0, len(stacks))
	for _, s := range stacks {
		records, err := telemetry.Inventory(s, "")
		if err != nil {
			return nil, fmt.Errorf("inventory for %s: %w", s, err)
		}
		if len(records) == 0 {
			continue
		}
		out = append(out, stackInventory{Stack: s, Records: records})
	}
	return out, nil
}

func runTelemetryStatus(stack string, asJSON bool) error {
	invs, err := gatherInventories(stack)
	if err != nil {
		return err
	}

	rows := flattenInventories(invs)

	if asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if rows == nil {
			rows = []statusRow{}
		}
		return enc.Encode(rows)
	}

	if len(rows) == 0 {
		if stack != "" {
			fmt.Printf("No persisted telemetry for stack %q\n", stack)
		} else {
			fmt.Println("No persisted telemetry")
		}
		return nil
	}

	renderStatusTable(os.Stdout, rows)
	return nil
}

// flattenInventories collapses per-stack inventory groups into the flat
// statusRow shape used for both the table and JSON output.
func flattenInventories(invs []stackInventory) []statusRow {
	var rows []statusRow
	for _, inv := range invs {
		for _, r := range inv.Records {
			rows = append(rows, statusRow{
				Stack:     inv.Stack,
				Server:    r.Server,
				Signal:    r.Signal,
				Path:      r.Path,
				SizeBytes: r.SizeBytes,
				FileCount: r.FileCount,
				Oldest:    formatInventoryTime(r.OldestTime),
				Newest:    formatInventoryTime(r.NewestTime),
			})
		}
	}
	return rows
}

func renderStatusTable(w io.Writer, rows []statusRow) {
	t := table.NewWriter()
	t.SetOutputMirror(w)
	t.SetStyle(table.StyleRounded)
	t.AppendHeader(table.Row{"STACK", "SERVER", "SIGNAL", "SIZE", "FILES", "OLDEST", "NEWEST"})
	for _, r := range rows {
		t.AppendRow(table.Row{
			r.Stack,
			r.Server,
			r.Signal,
			formatBytes(r.SizeBytes),
			r.FileCount,
			r.Oldest,
			r.Newest,
		})
	}
	t.Render()
}

func runTelemetryWipe(stack, server, signal string, yes bool) error {
	if signal != "" && !telemetry.IsValidSignal(signal) {
		return fmt.Errorf("invalid signal %q (expected logs, metrics, or traces)", signal)
	}

	invs, err := gatherInventories(stack)
	if err != nil {
		return err
	}

	// Filter the inventory by --server and --signal so the confirmation
	// prompt enumerates exactly what Wipe will delete.
	matching := filterInventories(invs, server, signal)
	if isInventoryEmpty(matching) {
		fmt.Println("Nothing to wipe — no matching telemetry found")
		return nil
	}

	if !yes {
		printWipeSummary(os.Stdout, matching, server, signal)
		fmt.Print("Proceed? [y/N] ")
		reader := bufio.NewReader(os.Stdin)
		ans, _ := reader.ReadString('\n')
		ans = strings.TrimSpace(strings.ToLower(ans))
		if ans != "y" && ans != "yes" {
			fmt.Println("Cancelled")
			return nil
		}
	}

	var wipeErrs []error
	for _, inv := range matching {
		if err := telemetry.Wipe(inv.Stack, server, signal); err != nil {
			wipeErrs = append(wipeErrs, fmt.Errorf("wipe %s: %w", inv.Stack, err))
		}
	}
	if len(wipeErrs) > 0 {
		return errors.Join(wipeErrs...)
	}

	totalBytes, totalFiles, _ := summarizeInventories(matching)
	fmt.Printf("Wiped %d %s (%s) across %d %s\n",
		totalFiles, plural(totalFiles, "file", "files"),
		formatBytes(totalBytes),
		len(matching), plural(len(matching), "stack", "stacks"))
	return nil
}

func plural(n int, singular, plural string) string {
	if n == 1 {
		return singular
	}
	return plural
}

// filterInventories keeps only records that match the requested server and
// signal, dropping any stack that ends up with no records. Empty filters act
// as wildcards.
func filterInventories(invs []stackInventory, server, signal string) []stackInventory {
	if server == "" && signal == "" {
		return invs
	}
	out := make([]stackInventory, 0, len(invs))
	for _, inv := range invs {
		var keep []telemetry.InventoryRecord
		for _, r := range inv.Records {
			if server != "" && r.Server != server {
				continue
			}
			if signal != "" && r.Signal != signal {
				continue
			}
			keep = append(keep, r)
		}
		if len(keep) > 0 {
			out = append(out, stackInventory{Stack: inv.Stack, Records: keep})
		}
	}
	return out
}

func isInventoryEmpty(invs []stackInventory) bool {
	for _, inv := range invs {
		if len(inv.Records) > 0 {
			return false
		}
	}
	return true
}

// summarizeInventories returns total bytes, total file count, and the unique
// set of servers across the supplied inventories. Used by both the wipe
// confirmation summary and the post-wipe output.
func summarizeInventories(invs []stackInventory) (int64, int, []string) {
	var bytes int64
	files := 0
	seen := map[string]struct{}{}
	var servers []string
	for _, inv := range invs {
		for _, r := range inv.Records {
			bytes += r.SizeBytes
			files += r.FileCount
			if _, ok := seen[r.Server]; !ok {
				seen[r.Server] = struct{}{}
				servers = append(servers, r.Server)
			}
		}
	}
	sort.Strings(servers)
	return bytes, files, servers
}

func printWipeSummary(w io.Writer, invs []stackInventory, server, signal string) {
	totalBytes, totalFiles, servers := summarizeInventories(invs)

	scope := "everything"
	if signal != "" {
		scope = signal
	}
	if server != "" {
		scope = "server " + server
		if signal != "" {
			scope = signal + " for server " + server
		}
	}

	stacks := make([]string, 0, len(invs))
	for _, inv := range invs {
		stacks = append(stacks, inv.Stack)
	}

	fmt.Fprintf(w, "About to delete %s persisted telemetry:\n", scope)
	fmt.Fprintf(w, "  Stacks:  %s\n", strings.Join(stacks, ", "))
	fmt.Fprintf(w, "  Servers: %s\n", strings.Join(servers, ", "))
	fmt.Fprintf(w, "  Files:   %d\n", totalFiles)
	fmt.Fprintf(w, "  Size:    %s\n", formatBytes(totalBytes))
}

func runTelemetryTail(stack, server, signal string) error {
	if !telemetry.IsValidSignal(signal) {
		return fmt.Errorf("invalid signal %q (expected logs, metrics, or traces)", signal)
	}

	path := state.TelemetryServerPath(stack, server, signal)
	dir := filepath.Dir(path)
	if _, err := os.Stat(dir); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("no telemetry directory for stack %q server %q (enable persistence in the stack YAML and start the daemon)", stack, server)
		}
		return fmt.Errorf("stat %s: %w", dir, err)
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("create watcher: %w", err)
	}
	defer watcher.Close()
	if err := watcher.Add(dir); err != nil {
		return fmt.Errorf("watch %s: %w", dir, err)
	}

	sigCh := make(chan os.Signal, 1)
	ossignal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	defer ossignal.Stop(sigCh)

	t := newTailReader(path, os.Stdout)
	if err := t.openAtEnd(); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("open %s: %w", path, err)
	}
	defer t.close()
	t.drain()

	target := filepath.Base(path)
	for {
		select {
		case <-sigCh:
			fmt.Println()
			return nil
		case ev, ok := <-watcher.Events:
			if !ok {
				return nil
			}
			if filepath.Base(ev.Name) != target {
				continue
			}
			switch {
			case ev.Has(fsnotify.Create):
				// Newly created (or recreated after rotation): read the whole
				// file from the start so the user does not miss the first
				// batch lumberjack flushes after rotating.
				_ = t.openAtStart()
				t.drain()
			case ev.Has(fsnotify.Write):
				if !t.isOpen() {
					_ = t.openAtStart()
				}
				t.drain()
			case ev.Has(fsnotify.Rename), ev.Has(fsnotify.Remove):
				// Lumberjack rotated or wiped the active file. Drop our
				// handle and wait for the Create event that brings the new
				// file in.
				t.close()
			}
		case err, ok := <-watcher.Errors:
			if !ok {
				return nil
			}
			fmt.Fprintf(os.Stderr, "warning: watcher error: %v\n", err)
		}
	}
}

// tailReader wraps a single open file and exposes the operations the tail
// loop needs — open at end (for the initial attach), open at start (after
// rotation), drain pending bytes line-by-line, and close on rotation/exit.
//
// Partial trailing bytes (a half-flushed line) are held in `partial` until a
// newline arrives; only complete lines are emitted. This matches `tail -f`
// semantics and keeps NDJSON consumers from seeing torn JSON objects.
type tailReader struct {
	path    string
	out     io.Writer
	file    *os.File
	reader  *bufio.Reader
	partial []byte
}

func newTailReader(path string, out io.Writer) *tailReader {
	return &tailReader{path: path, out: out}
}

func (t *tailReader) isOpen() bool { return t.file != nil }

func (t *tailReader) close() {
	if t.file != nil {
		_ = t.file.Close()
		t.file = nil
		t.reader = nil
	}
	t.partial = t.partial[:0]
}

func (t *tailReader) openAtEnd() error {
	t.close()
	f, err := os.Open(t.path)
	if err != nil {
		return err
	}
	if _, err := f.Seek(0, io.SeekEnd); err != nil {
		_ = f.Close()
		return err
	}
	t.file = f
	t.reader = bufio.NewReader(f)
	return nil
}

func (t *tailReader) openAtStart() error {
	t.close()
	f, err := os.Open(t.path)
	if err != nil {
		return err
	}
	t.file = f
	t.reader = bufio.NewReader(f)
	return nil
}

// drain reads complete lines from the underlying reader and writes them to
// out. Partial trailing bytes are accumulated in t.partial and flushed only
// when a newline arrives. Errors other than io.EOF are surfaced as a warning
// so the tail loop never exits silently on a transient read failure.
func (t *tailReader) drain() {
	if t.reader == nil {
		return
	}
	for {
		chunk, err := t.reader.ReadString('\n')
		if chunk != "" {
			t.partial = append(t.partial, chunk...)
		}
		if err == nil {
			// chunk ended in '\n' — flush the accumulated line.
			fmt.Fprint(t.out, string(t.partial))
			t.partial = t.partial[:0]
			continue
		}
		if err != io.EOF {
			fmt.Fprintf(os.Stderr, "warning: read %s: %v\n", t.path, err)
		}
		return
	}
}

// formatBytes renders byte counts using binary multiples (KiB / MiB / GiB) to
// match the frontend formatter in web/src/lib/format-bytes.ts. Operators
// reading the wipe summary on the CLI and the wipe modal in the UI see the
// same units for the same number.
func formatBytes(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	units := []string{"KiB", "MiB", "GiB", "TiB"}
	if exp >= len(units) {
		exp = len(units) - 1
	}
	value := float64(bytes) / float64(div)
	if value < 10 {
		return fmt.Sprintf("%.1f %s", value, units[exp])
	}
	return fmt.Sprintf("%d %s", int64(value+0.5), units[exp])
}

// formatInventoryTime renders an InventoryRecord timestamp as YYYY-MM-DD
// HH:MM. Zero-valued times become "—" so the table column never reads
// "0001-01-01 ..." for stacks with no rotated siblings yet.
func formatInventoryTime(t time.Time) string {
	if t.IsZero() {
		return "—"
	}
	return t.Format("2006-01-02 15:04")
}
