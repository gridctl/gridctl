package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/gridctl/gridctl/pkg/state"
	"github.com/gridctl/gridctl/pkg/tracing"
	"github.com/spf13/cobra"
)

const (
	tracesHTTPTimeout  = 5 * time.Second
	tracesFollowPeriod = 2 * time.Second
	tracesBarWidth     = 40
	tracesNameWidth    = 28
)

var (
	tracesStack       string
	tracesServer      string
	tracesErrorsOnly  bool
	tracesMinDuration string
	tracesJSON        bool
	tracesFollow      bool
)

var tracesCmd = &cobra.Command{
	Use:   "traces [trace-id]",
	Short: "Show distributed traces from the MCP gateway",
	Long: `Displays distributed traces collected by the MCP gateway.

Without arguments, shows a table of recent traces.
With a trace ID, shows the full span waterfall.`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		port, err := resolveTracesPort(tracesStack)
		if err != nil {
			return err
		}
		if len(args) == 1 {
			return runTraceDetail(port, args[0])
		}
		if tracesFollow {
			return runTracesFollow(port)
		}
		return runTracesList(port)
	},
}

func init() {
	tracesCmd.Flags().StringVarP(&tracesStack, "stack", "s", "", "Stack to query (defaults to first running stack)")
	tracesCmd.Flags().StringVar(&tracesServer, "server", "", "Filter by server name")
	tracesCmd.Flags().BoolVar(&tracesErrorsOnly, "errors", false, "Show only error traces")
	tracesCmd.Flags().StringVar(&tracesMinDuration, "min-duration", "", "Minimum trace duration (e.g. 100ms, 1s)")
	tracesCmd.Flags().BoolVar(&tracesJSON, "json", false, "Output as JSON")
	tracesCmd.Flags().BoolVar(&tracesFollow, "follow", false, "Stream new traces as they arrive")
	rootCmd.AddCommand(tracesCmd)
}

// resolveTracesPort finds the port of a running gateway, optionally filtered by stack name.
func resolveTracesPort(stackName string) (int, error) {
	states, err := state.List()
	if err != nil {
		return 0, fmt.Errorf("traces: could not read state: %w", err)
	}
	for _, s := range states {
		if (stackName == "" || s.StackName == stackName) && state.IsRunning(&s) {
			return s.Port, nil
		}
	}
	if stackName != "" {
		return 0, fmt.Errorf("traces: stack %q not found or not running", stackName)
	}
	return 0, fmt.Errorf("traces: no running gateway — start one with 'gridctl deploy'")
}

// buildTracesURL constructs the /api/traces URL with the current filter flags.
func buildTracesURL(port int) string {
	base := fmt.Sprintf("http://localhost:%d/api/traces", port)
	var params []string
	if tracesServer != "" {
		params = append(params, "server="+tracesServer)
	}
	if tracesErrorsOnly {
		params = append(params, "errors=true")
	}
	if tracesMinDuration != "" {
		params = append(params, "min_duration="+tracesMinDuration)
	}
	if len(params) > 0 {
		return base + "?" + strings.Join(params, "&")
	}
	return base
}

// fetchTraces retrieves traces from the gateway API.
func fetchTraces(port int) ([]tracing.TraceRecord, error) {
	client := &http.Client{Timeout: tracesHTTPTimeout}
	resp, err := client.Get(buildTracesURL(port))
	if err != nil {
		return nil, fmt.Errorf("traces: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("traces: reading response: %w", err)
	}
	var records []tracing.TraceRecord
	if err := json.Unmarshal(body, &records); err != nil {
		return nil, fmt.Errorf("traces: parsing response: %w", err)
	}
	return records, nil
}

// runTracesList prints a table of recent traces.
func runTracesList(port int) error {
	records, err := fetchTraces(port)
	if err != nil {
		return err
	}
	if tracesJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(records)
	}
	if len(records) == 0 {
		fmt.Println("No traces yet")
		return nil
	}
	printTracesTable(os.Stdout, records)
	return nil
}

// printTracesTable writes a tab-aligned trace list table to w.
func printTracesTable(w io.Writer, records []tracing.TraceRecord) {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "TRACE ID\tDURATION\tSPANS\tSTATUS\tOPERATION")
	for _, tr := range records {
		printTraceRow(tw, tr)
	}
	_ = tw.Flush()
}

// printTraceRow writes one trace row to a tabwriter.
func printTraceRow(w io.Writer, tr tracing.TraceRecord) {
	status := "ok"
	if tr.IsError {
		status = "error"
	}
	traceID := tr.TraceID
	if len(traceID) > 16 {
		traceID = traceID[:16]
	}
	fmt.Fprintf(w, "%s\t%s\t%d\t%s\t%s\n",
		traceID, formatTraceDuration(tr.DurationMs), tr.SpanCount, status, tr.Operation)
}

// runTracesFollow polls for new traces and streams them to stdout until interrupted.
func runTracesFollow(port int) error {
	seen := make(map[string]struct{})
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	headerPrinted := false

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)
	defer signal.Stop(sig)

	ticker := time.NewTicker(tracesFollowPeriod)
	defer ticker.Stop()

	poll := func() {
		records, err := fetchTraces(port)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: %v\n", err)
			return
		}
		var unseen []tracing.TraceRecord
		for _, tr := range records {
			if _, ok := seen[tr.TraceID]; !ok {
				unseen = append(unseen, tr)
				seen[tr.TraceID] = struct{}{}
			}
		}
		if len(unseen) == 0 {
			return
		}
		// API returns newest first; reverse for chronological output.
		for i, j := 0, len(unseen)-1; i < j; i, j = i+1, j-1 {
			unseen[i], unseen[j] = unseen[j], unseen[i]
		}
		if !headerPrinted {
			fmt.Fprintln(tw, "TRACE ID\tDURATION\tSPANS\tSTATUS\tOPERATION")
			headerPrinted = true
		}
		for _, tr := range unseen {
			printTraceRow(tw, tr)
		}
		_ = tw.Flush()
	}

	poll() // initial fetch
	for {
		select {
		case <-ticker.C:
			poll()
		case <-sig:
			return nil
		}
	}
}

// runTraceDetail fetches and renders an ASCII waterfall for a single trace.
func runTraceDetail(port int, traceID string) error {
	url := fmt.Sprintf("http://localhost:%d/api/traces/%s", port, traceID)
	client := &http.Client{Timeout: tracesHTTPTimeout}
	resp, err := client.Get(url)
	if err != nil {
		return fmt.Errorf("traces: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return fmt.Errorf("traces: trace %s not found", traceID)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("traces: reading response: %w", err)
	}
	if tracesJSON {
		var raw json.RawMessage
		if err := json.Unmarshal(body, &raw); err != nil {
			return fmt.Errorf("traces: parsing response: %w", err)
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(raw)
	}
	var tr tracing.TraceRecord
	if err := json.Unmarshal(body, &tr); err != nil {
		return fmt.Errorf("traces: parsing response: %w", err)
	}
	printWaterfall(os.Stdout, tr)
	return nil
}

// printWaterfall renders an ASCII span waterfall for a trace.
func printWaterfall(w io.Writer, tr tracing.TraceRecord) {
	short := tr.TraceID
	if len(short) > 12 {
		short = short[:12]
	}
	fmt.Fprintf(w, "Trace %s (%s, %d spans)\n", short, formatTraceDuration(tr.DurationMs), tr.SpanCount)
	if len(tr.Spans) == 0 {
		return
	}

	// Sort spans by start time.
	spans := make([]tracing.SpanRecord, len(tr.Spans))
	copy(spans, tr.Spans)
	sort.Slice(spans, func(i, j int) bool {
		return spans[i].StartTime.Before(spans[j].StartTime)
	})

	total := tr.DurationMs
	if total <= 0 {
		total = 1
	}

	for i, span := range spans {
		isLast := i == len(spans)-1
		connector := "├─"
		if isLast {
			connector = "└─"
		}

		offsetMs := span.StartTime.Sub(tr.StartTime).Milliseconds()
		if offsetMs < 0 {
			offsetMs = 0
		}
		endMs := offsetMs + span.DurationMs

		startPos := int(float64(offsetMs) / float64(total) * tracesBarWidth)
		barLen := int(float64(span.DurationMs) / float64(total) * tracesBarWidth)
		if barLen < 1 {
			barLen = 1
		}
		if startPos+barLen > tracesBarWidth {
			barLen = tracesBarWidth - startPos
			if barLen < 1 {
				barLen = 1
				startPos = tracesBarWidth - 1
			}
		}

		name := span.Name
		if len(name) > tracesNameWidth {
			name = name[:tracesNameWidth-1] + "…"
		}
		paddedName := fmt.Sprintf("%-*s", tracesNameWidth, name)

		fmt.Fprintf(w, "%s %s %dms%s%s┤ %dms\n",
			connector, paddedName, offsetMs,
			strings.Repeat(" ", startPos),
			strings.Repeat("─", barLen),
			endMs,
		)

		// Print notable span attributes as a sub-line.
		if transport, ok := span.Attrs["network.transport"]; ok {
			indent := "│  └─"
			if isLast {
				indent = "   └─"
			}
			parts := []string{"transport: " + transport}
			if srv, ok2 := span.Attrs["server.name"]; ok2 {
				parts = append(parts, "server: "+srv)
			}
			fmt.Fprintf(w, "%s %s\n", indent, strings.Join(parts, ", "))
		}
	}
}

// formatTraceDuration formats a millisecond count for display.
func formatTraceDuration(ms int64) string {
	if ms < 1000 {
		return fmt.Sprintf("%dms", ms)
	}
	return fmt.Sprintf("%.1fs", float64(ms)/1000)
}
