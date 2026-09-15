package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/gridctl/gridctl/pkg/runs"
	"github.com/spf13/cobra"
)

var (
	runsStack       string
	runsFile        string
	runsOffline     bool
	runsFormat      string
	runsListJSON    bool
	runsStatusJSON  bool
	runsSince       string
	runsUntil       string
	runsRequested   string
	runsServer      string
	runsTool        string
	runsDisposition string
	runsClient      string
	runsAccess      string
	runsAttempt     string
	runsLimit       int
	runsWipeYes     bool
)

var runsCmd = &cobra.Command{
	Use:   "runs",
	Short: "Query metadata-only persisted dispatch records",
	Long: `Query retained records of returning tool-dispatch attempts.

Recording is opt-in via stack.yaml runs.enabled. Records are metadata-only
and best-effort: a crash may leave no record, and older records may have
been removed by retention or wipe.

Live queries talk to the running daemon. --file and --offline read local
files only. A failed live request never falls back to disk, and an
unreadable --file is never replaced by another source.`,
}

var runsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List persisted run records",
	RunE: func(cmd *cobra.Command, args []string) error {
		format, err := resolveFormat(runsFormat, cmd.Flags().Changed("format"), runsListJSON)
		if err != nil {
			return err
		}
		return runRunsList(cmd.Context(), format)
	},
}

var runsStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show run recorder and history health",
	RunE: func(cmd *cobra.Command, args []string) error {
		format, err := resolveFormat(runsFormat, cmd.Flags().Changed("format"), runsStatusJSON)
		if err != nil {
			return err
		}
		return runRunsStatus(cmd.Context(), format)
	},
}

var runsWipeCmd = &cobra.Command{
	Use:   "wipe",
	Short: "Delete persisted run records for a stack",
	Long: `Deletes stack-wide run history. Per-server deletion is not supported.

Wipe is not secure erasure and does not remove exports or backups.
Recording remains enabled if it was already on; new post-wipe records may appear.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runRunsWipe(cmd.Context())
	},
}

func init() {
	for _, cmd := range []*cobra.Command{runsListCmd, runsStatusCmd, runsWipeCmd} {
		cmd.Flags().StringVarP(&runsStack, "stack", "s", "", "Stack name")
	}
	runsListCmd.Flags().StringVar(&runsFile, "file", "", "Read an explicit offline JSONL file or directory")
	runsListCmd.Flags().BoolVar(&runsOffline, "offline", false, "Read the stack's on-disk files without contacting the daemon")
	runsListCmd.Flags().StringVar(&runsFormat, "format", "", "Output format (json)")
	runsListCmd.Flags().BoolVar(&runsListJSON, "json", false, "Shorthand for --format json")
	runsListCmd.Flags().StringVar(&runsSince, "since", "", "RFC3339 lower bound on returned_at")
	runsListCmd.Flags().StringVar(&runsUntil, "until", "", "RFC3339 upper bound on returned_at")
	runsListCmd.Flags().StringVar(&runsRequested, "requested", "", "Filter by requested name")
	runsListCmd.Flags().StringVar(&runsServer, "server", "", "Filter by resolved server")
	runsListCmd.Flags().StringVar(&runsTool, "tool", "", "Filter by resolved tool")
	runsListCmd.Flags().StringVar(&runsDisposition, "disposition", "", "Filter by disposition")
	runsListCmd.Flags().StringVar(&runsClient, "client", "", "Filter by caller-declared client label")
	runsListCmd.Flags().StringVar(&runsAccess, "access", "", "Filter by caller-declared access label")
	runsListCmd.Flags().StringVar(&runsAttempt, "attempt", "", "Filter by attempt ID")
	runsListCmd.Flags().IntVar(&runsLimit, "limit", runs.DefaultQueryLimit, "Maximum records to return")

	runsStatusCmd.Flags().StringVar(&runsFile, "file", "", "Inspect an explicit offline path")
	runsStatusCmd.Flags().BoolVar(&runsOffline, "offline", false, "Read on-disk files without contacting the daemon")
	runsStatusCmd.Flags().StringVar(&runsFormat, "format", "", "Output format (json)")
	runsStatusCmd.Flags().BoolVar(&runsStatusJSON, "json", false, "Shorthand for --format json")

	runsWipeCmd.Flags().BoolVarP(&runsWipeYes, "yes", "y", false, "Skip confirmation")
	runsWipeCmd.Flags().StringVar(&runsFile, "file", "", "unused")
	_ = runsWipeCmd.Flags().MarkHidden("file")

	runsCmd.AddCommand(runsListCmd)
	runsCmd.AddCommand(runsStatusCmd)
	runsCmd.AddCommand(runsWipeCmd)
}

func runsFilter() (runs.Filter, error) {
	f := runs.Filter{
		RequestedName:  runsRequested,
		ResolvedServer: runsServer,
		ResolvedTool:   runsTool,
		Disposition:    runsDisposition,
		ClientLabel:    runsClient,
		AccessLabel:    runsAccess,
		AttemptID:      runsAttempt,
	}
	var err error
	if runsSince != "" {
		f.Since, err = time.Parse(time.RFC3339, runsSince)
		if err != nil {
			return f, fmt.Errorf("invalid --since")
		}
	}
	if runsUntil != "" {
		f.Until, err = time.Parse(time.RFC3339, runsUntil)
		if err != nil {
			return f, fmt.Errorf("invalid --until")
		}
	}
	return f, nil
}

func runRunsList(ctx context.Context, format string) error {
	if runsFile != "" && runsOffline {
		return fmt.Errorf("use either --file or --offline, not both")
	}
	filter, err := runsFilter()
	if err != nil {
		return err
	}
	var result runs.QueryResult
	if runsFile != "" {
		result, err = runs.QueryPath(ctx, runsFile, filter, runsLimit, nil)
		if err != nil {
			return fmt.Errorf("runs: cannot read %s", runsFile)
		}
	} else if runsOffline {
		if runsStack == "" {
			return fmt.Errorf("runs: --offline requires --stack")
		}
		dir, err := runs.Dir(runsStack)
		if err != nil {
			return err
		}
		result, err = runs.Query(ctx, dir, filter, runsLimit, nil, 0)
		if err != nil {
			return fmt.Errorf("runs: cannot read offline history")
		}
	} else {
		return runRunsListLive(format, filter)
	}
	return emitRunsResult(format, result)
}

func runRunsListLive(format string, filter runs.Filter) error {
	port, err := resolveRunningPort("runs", runsStack)
	if err != nil {
		return err
	}
	q := url.Values{}
	if filter.RequestedName != "" {
		q.Set("requested", filter.RequestedName)
	}
	if filter.ResolvedServer != "" {
		q.Set("server", filter.ResolvedServer)
	}
	if filter.ResolvedTool != "" {
		q.Set("tool", filter.ResolvedTool)
	}
	if filter.Disposition != "" {
		q.Set("disposition", filter.Disposition)
	}
	if filter.ClientLabel != "" {
		q.Set("client", filter.ClientLabel)
	}
	if filter.AccessLabel != "" {
		q.Set("access", filter.AccessLabel)
	}
	if filter.AttemptID != "" {
		q.Set("attempt", filter.AttemptID)
	}
	if !filter.Since.IsZero() {
		q.Set("since", filter.Since.Format(time.RFC3339))
	}
	if !filter.Until.IsZero() {
		q.Set("until", filter.Until.Format(time.RFC3339))
	}
	if runsLimit > 0 {
		q.Set("limit", fmt.Sprintf("%d", runsLimit))
	}
	api := newDaemonAPI(port, 10*time.Second)
	u := api.URL("/api/runs")
	if enc := q.Encode(); enc != "" {
		u += "?" + enc
	}
	resp, err := api.Get(u)
	if err != nil {
		return fmt.Errorf("runs: live query failed")
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("runs: live query failed")
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("runs: live query failed (%s)", strings.TrimSpace(string(body)))
	}
	if format == "json" {
		if _, err := os.Stdout.Write(body); err != nil {
			return err
		}
		if len(body) > 0 && body[len(body)-1] != '\n' {
			fmt.Fprintln(os.Stdout)
		}
		var probe struct {
			Partial bool `json:"partial"`
		}
		_ = json.Unmarshal(body, &probe)
		if probe.Partial {
			fmt.Fprintln(os.Stderr, "runs: partial history")
			os.Exit(2)
		}
		return nil
	}
	var dto struct {
		Records  []map[string]any `json:"records"`
		Warnings []runs.Warning   `json:"warnings"`
		Partial  bool             `json:"partial"`
	}
	if err := json.Unmarshal(body, &dto); err != nil {
		return fmt.Errorf("runs: live query failed")
	}
	printRunsWarnings(dto.Warnings)
	printRunsTable(dto.Records)
	if dto.Partial {
		os.Exit(2)
	}
	return nil
}

func emitRunsResult(format string, result runs.QueryResult) error {
	printRunsWarnings(result.Warnings)
	if format == "json" {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(result); err != nil {
			return err
		}
		if result.Partial {
			os.Exit(2)
		}
		return nil
	}
	rows := make([]map[string]any, 0, len(result.Records))
	for _, rec := range result.Records {
		rows = append(rows, map[string]any{
			"returnedAt":     rec.ReturnedAt.Format(time.RFC3339),
			"requestedName":  rec.RequestedName,
			"resolvedServer": rec.ResolvedServer,
			"resolvedTool":   rec.ResolvedTool,
			"disposition":    rec.Disposition,
			"durationMs":     rec.DurationMS,
		})
	}
	printRunsTable(rows)
	if result.Partial {
		os.Exit(2)
	}
	return nil
}

func printRunsWarnings(warnings []runs.Warning) {
	for _, w := range warnings {
		fmt.Fprintf(os.Stderr, "runs: %s (%d)\n", w.Message, w.Count)
	}
}

func printRunsTable(rows []map[string]any) {
	if len(rows) == 0 {
		fmt.Fprintln(os.Stderr, "No matching run records")
		return
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "RETURNED\tTARGET\tDISPOSITION\tDURATION")
	for _, row := range rows {
		target := fmt.Sprint(row["requestedName"])
		if s, _ := row["resolvedServer"].(string); s != "" {
			if t, _ := row["resolvedTool"].(string); t != "" {
				target = s + " › " + t
			}
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%vms\n", row["returnedAt"], target, row["disposition"], row["durationMs"])
	}
	_ = tw.Flush()
}

func runRunsStatus(ctx context.Context, format string) error {
	if runsFile != "" {
		invPath := runsFile
		info, err := os.Stat(invPath)
		if err != nil {
			return fmt.Errorf("runs: cannot read %s", runsFile)
		}
		st := runs.Status{Enabled: false, Effective: false, WriterHealth: runs.HealthStopped, HistoricalLoss: runs.LossUnknown}
		payload := map[string]any{"status": st, "path": invPath, "dir": info.IsDir()}
		if format == "json" {
			return json.NewEncoder(os.Stdout).Encode(payload)
		}
		fmt.Fprintf(os.Stderr, "Offline path %s (recorder state unknown)\n", runsFile)
		fmt.Printf("writer: %s  historical_loss: %s\n", st.WriterHealth, st.HistoricalLoss)
		return nil
	}
	if runsOffline {
		if runsStack == "" {
			return fmt.Errorf("runs: --offline requires --stack")
		}
		inv, err := runs.StackInventory(runsStack)
		if err != nil {
			return fmt.Errorf("runs: cannot read offline history")
		}
		st := runs.Status{Enabled: false, Effective: false, WriterHealth: runs.HealthStopped, HistoricalLoss: runs.LossUnknown}
		if format == "json" {
			return json.NewEncoder(os.Stdout).Encode(map[string]any{"status": st, "inventory": inv})
		}
		fmt.Fprintf(os.Stderr, "Offline inventory for %s (recorder state unknown)\n", runsStack)
		fmt.Printf("files: %d  bytes: %d  writer: %s\n", inv.FileCount, inv.SizeBytes, st.WriterHealth)
		return nil
	}
	port, err := resolveRunningPort("runs", runsStack)
	if err != nil {
		return err
	}
	api := newDaemonAPI(port, 10*time.Second)
	resp, err := api.Get(api.URL("/api/runs/status"))
	if err != nil {
		return fmt.Errorf("runs: live status failed")
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("runs: live status failed")
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("runs: live status failed")
	}
	if format == "json" {
		if _, err := os.Stdout.Write(body); err != nil {
			return err
		}
		if len(body) > 0 && body[len(body)-1] != '\n' {
			fmt.Fprintln(os.Stdout)
		}
		return nil
	}
	var st struct {
		Enabled      bool   `json:"enabled"`
		Effective    bool   `json:"effective"`
		WriterHealth string `json:"writer_health"`
		QueueDepth   int    `json:"queue_depth"`
	}
	_ = json.Unmarshal(body, &st)
	fmt.Printf("enabled: %v  effective: %v  writer: %s  queue: %d\n", st.Enabled, st.Effective, st.WriterHealth, st.QueueDepth)
	return nil
}

func runRunsWipe(ctx context.Context) error {
	if runsStack == "" {
		return fmt.Errorf("runs wipe requires --stack")
	}
	if !runsWipeYes {
		fmt.Fprintf(os.Stderr, "Delete run history for stack %q? This is not secure erasure. [y/N] ", runsStack)
		var reply string
		_, _ = fmt.Scanln(&reply)
		if strings.ToLower(strings.TrimSpace(reply)) != "y" {
			return fmt.Errorf("aborted")
		}
	}
	port, err := resolveRunningPort("runs", runsStack)
	if err == nil {
		api := newDaemonAPI(port, 15*time.Second)
		resp, err := api.Do(http.MethodPost, api.URL("/api/runs/wipe"), nil)
		if err != nil {
			return fmt.Errorf("runs: live wipe failed")
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("runs: live wipe failed")
		}
		var out struct {
			Success          bool   `json:"success"`
			Partial          bool   `json:"partial"`
			RecordingEnabled bool   `json:"recordingEnabled"`
			Scope            string `json:"scope"`
		}
		_ = json.Unmarshal(body, &out)
		if !out.Success {
			fmt.Fprintln(os.Stderr, "runs: wipe incomplete")
			os.Exit(2)
		}
		fmt.Fprintf(os.Stderr, "Wiped run history for %s. Recording enabled: %v. Wipe is not secure erasure.\n", out.Scope, out.RecordingEnabled)
		return nil
	}
	if err := runs.WipeStack(ctx, runsStack); err != nil {
		if err == runs.ErrActiveWriter {
			return err
		}
		fmt.Fprintln(os.Stderr, "runs: wipe incomplete")
		os.Exit(2)
	}
	fmt.Fprintf(os.Stderr, "Wiped offline run history for %s. Wipe is not secure erasure.\n", runsStack)
	return nil
}
