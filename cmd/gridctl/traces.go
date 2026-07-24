package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

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

// apiTraceSummary mirrors the wire shape served by GET /api/traces. The API
// serves camelCase DTOs, not the internal pkg/tracing structs; decoding into
// anything else silently drifts (see the contract tests in internal/api).
type apiTraceSummary struct {
	TraceID   string    `json:"traceId"`
	Operation string    `json:"operation"`
	Tool      string    `json:"tool"`
	Client    string    `json:"client"`
	Server    string    `json:"server"`
	StartTime time.Time `json:"startTime"`
	Duration  int64     `json:"duration"`
	SpanCount int       `json:"spanCount"`
	HasError  bool      `json:"hasError"`
	Status    string    `json:"status"`
}

// apiTraceList mirrors the envelope served by GET /api/traces.
type apiTraceList struct {
	Traces         []apiTraceSummary `json:"traces"`
	Total          int               `json:"total"`
	TracingEnabled bool              `json:"tracingEnabled"`
	BufferSize     int               `json:"bufferSize"`
	BufferCapacity int               `json:"bufferCapacity"`
}

// apiSpan mirrors the span shape served by GET /api/traces/{traceId}.
type apiSpan struct {
	SpanID       string            `json:"spanId"`
	ParentSpanID string            `json:"parentSpanId"`
	Name         string            `json:"name"`
	StartTime    time.Time         `json:"startTime"`
	EndTime      time.Time         `json:"endTime"`
	Duration     int64             `json:"duration"`
	Status       string            `json:"status"`
	Attributes   map[string]string `json:"attributes"`
}

// apiTraceDetail mirrors the envelope served by GET /api/traces/{traceId}.
type apiTraceDetail struct {
	TraceID string    `json:"traceId"`
	Spans   []apiSpan `json:"spans"`
}

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
}

// resolveTracesPort delegates to the shared running-port resolver with this
// command's error vocabulary.
func resolveTracesPort(stackName string) (int, error) {
	return resolveRunningPort("traces", stackName)
}

// buildTracesURL constructs the /api/traces URL with the current filter flags.
func buildTracesURL(port int) string {
	base := fmt.Sprintf("http://localhost:%d/api/traces", port)
	params := url.Values{}
	if tracesServer != "" {
		params.Set("server", tracesServer)
	}
	if tracesErrorsOnly {
		params.Set("errors", "true")
	}
	if tracesMinDuration != "" {
		params.Set("minDuration", tracesMinDuration)
	}
	if len(params) > 0 {
		return base + "?" + params.Encode()
	}
	return base
}

// fetchTraces retrieves the trace list envelope from the gateway API.
func fetchTraces(port int) (apiTraceList, error) {
	var list apiTraceList
	client := &http.Client{Timeout: tracesHTTPTimeout}
	resp, err := client.Get(buildTracesURL(port))
	if err != nil {
		return list, fmt.Errorf("traces: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return list, fmt.Errorf("traces: reading response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return list, fmt.Errorf("traces: %s", strings.TrimSpace(string(body)))
	}
	if err := json.Unmarshal(body, &list); err != nil {
		return list, fmt.Errorf("traces: parsing response: %w", err)
	}
	return list, nil
}

// runTracesList prints a table of recent traces.
func runTracesList(port int) error {
	list, err := fetchTraces(port)
	if err != nil {
		return err
	}
	if tracesJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(list)
	}
	if len(list.Traces) == 0 {
		if !list.TracingEnabled {
			fmt.Println("Tracing is disabled (enable gateway.tracing in stack.yaml)")
		} else {
			fmt.Println("No traces yet")
		}
		return nil
	}
	printTracesTable(os.Stdout, list.Traces)
	return nil
}

// printTracesTable writes a tab-aligned trace list table to w.
func printTracesTable(w io.Writer, records []apiTraceSummary) {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "TRACE ID\tDURATION\tSPANS\tSTATUS\tOPERATION")
	for _, tr := range records {
		printTraceRow(tw, tr)
	}
	_ = tw.Flush()
}

// printTraceRow writes one trace row to a tabwriter.
func printTraceRow(w io.Writer, tr apiTraceSummary) {
	traceID := tr.TraceID
	if len(traceID) > 16 {
		traceID = traceID[:16]
	}
	fmt.Fprintf(w, "%s\t%s\t%d\t%s\t%s\n",
		traceID, formatTraceDuration(tr.Duration), tr.SpanCount, tr.Status, tr.Operation)
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
		list, err := fetchTraces(port)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: %v\n", err)
			return
		}
		var unseen []apiTraceSummary
		for _, tr := range list.Traces {
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
	detailURL := fmt.Sprintf("http://localhost:%d/api/traces/%s", port, url.PathEscape(traceID))
	client := &http.Client{Timeout: tracesHTTPTimeout}
	resp, err := client.Get(detailURL)
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
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("traces: %s", strings.TrimSpace(string(body)))
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
	var detail apiTraceDetail
	if err := json.Unmarshal(body, &detail); err != nil {
		return fmt.Errorf("traces: parsing response: %w", err)
	}
	printWaterfall(os.Stdout, detail)
	return nil
}

// spanEnd returns the span's end time, deriving it from start + duration when
// the API omitted endTime.
func spanEnd(sp apiSpan) time.Time {
	if !sp.EndTime.IsZero() {
		return sp.EndTime
	}
	return sp.StartTime.Add(time.Duration(sp.Duration) * time.Millisecond)
}

// printWaterfall renders an ASCII span waterfall for a trace.
func printWaterfall(w io.Writer, detail apiTraceDetail) {
	short := detail.TraceID
	if len(short) > 12 {
		short = short[:12]
	}
	if len(detail.Spans) == 0 {
		fmt.Fprintf(w, "Trace %s (0ms, 0 spans)\n", short)
		return
	}

	// Sort spans by start time.
	spans := make([]apiSpan, len(detail.Spans))
	copy(spans, detail.Spans)
	sort.Slice(spans, func(i, j int) bool {
		return spans[i].StartTime.Before(spans[j].StartTime)
	})

	traceStart := spans[0].StartTime
	traceEnd := spanEnd(spans[0])
	for _, sp := range spans[1:] {
		if end := spanEnd(sp); end.After(traceEnd) {
			traceEnd = end
		}
	}
	total := traceEnd.Sub(traceStart).Milliseconds()
	fmt.Fprintf(w, "Trace %s (%s, %d spans)\n", short, formatTraceDuration(total), len(spans))
	if total <= 0 {
		total = 1
	}

	for i, span := range spans {
		isLast := i == len(spans)-1
		connector := "├─"
		if isLast {
			connector = "└─"
		}

		offsetMs := span.StartTime.Sub(traceStart).Milliseconds()
		if offsetMs < 0 {
			offsetMs = 0
		}
		endMs := offsetMs + span.Duration

		startPos := int(float64(offsetMs) / float64(total) * tracesBarWidth)
		barLen := int(float64(span.Duration) / float64(total) * tracesBarWidth)
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
		if transport, ok := span.Attributes["network.transport"]; ok {
			indent := "│  └─"
			if isLast {
				indent = "   └─"
			}
			parts := []string{"transport: " + transport}
			if srv, ok2 := span.Attributes["server.name"]; ok2 {
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
