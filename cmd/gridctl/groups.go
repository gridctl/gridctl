package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gridctl/gridctl/pkg/mcp"
	"github.com/gridctl/gridctl/pkg/output"

	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/spf13/cobra"
)

const groupsHTTPTimeout = 10 * time.Second

// Exit codes — matched against the limits/optimize conventions.
const (
	groupsExitOK             = 0
	groupsExitInfrastructure = 2
)

var (
	groupsStack   string
	groupsFormat  string
	groupsVerbose bool
	groupsJSON    *bool
	groupsPlain   *bool
)

var groupsCmd = &cobra.Command{
	Use:   "groups",
	Short: "Show tool groups and their resolved surfaces",
	Long: `Show every tool group declared under 'groups:' in stack.yaml with its
endpoint, resolved member count against the live tool surface, and
overrides. Groups are the curation axis: each serves a bundle of tools at
/groups/{name}/mcp, with optional renames, description rewrites, and
annotation hints. Per-client scoping and limits apply unchanged on group
sessions.

Link a client to a group with 'gridctl link <client> --group <name>'.

Default output is a styled table; use '--format json' to emit the same
report the API returns.

Exit codes:
  0  success (including no groups configured)
  2  infrastructure error (gateway unreachable)`,
	Example: `  gridctl groups              List groups with member counts
  gridctl groups --verbose    Include each group's exposed tool names
  gridctl groups --json       Machine-readable report`,
	RunE: func(cmd *cobra.Command, args []string) error {
		var err error
		if groupsFormat, err = resolveFormat(groupsFormat, cmd.Flags().Changed("format"), *groupsJSON); err != nil {
			return err
		}
		if err := resolvePlain(*groupsPlain, groupsFormat); err != nil {
			return err
		}
		port, err := resolveGroupsPort(groupsStack)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(groupsExitInfrastructure)
		}

		report, err := fetchGroupsReport(port)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(groupsExitInfrastructure)
		}

		if strings.EqualFold(groupsFormat, "json") {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			if err := enc.Encode(report); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(groupsExitInfrastructure)
			}
			return nil
		}
		renderGroupsTable(os.Stdout, report, *groupsPlain, groupsVerbose)
		return nil
	},
}

func init() {
	groupsCmd.Flags().StringVarP(&groupsStack, "stack", "s", "", "Stack to query (auto-detected when only one stack is running)")
	groupsCmd.Flags().StringVar(&groupsFormat, "format", "", "Output format: 'json' for machine-readable output (default: table)")
	groupsCmd.Flags().BoolVarP(&groupsVerbose, "verbose", "v", false, "Include each group's exposed tool names")
	groupsJSON = addJSONAlias(groupsCmd)
	groupsPlain = addPlainFlag(groupsCmd)
}

// resolveGroupsPort delegates to the shared running-port resolver with this
// command's error vocabulary.
func resolveGroupsPort(stackName string) (int, error) {
	return resolveRunningPort("groups", stackName)
}

// fetchGroupsReport calls GET /api/groups on the local gateway.
func fetchGroupsReport(port int) (mcp.GroupsReport, error) {
	client := &http.Client{Timeout: groupsHTTPTimeout}
	resp, err := client.Get(fmt.Sprintf("http://localhost:%d/api/groups", port))
	if err != nil {
		return mcp.GroupsReport{}, fmt.Errorf("groups: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return mcp.GroupsReport{}, fmt.Errorf("groups: reading response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return mcp.GroupsReport{}, fmt.Errorf("groups: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	var report mcp.GroupsReport
	if err := json.Unmarshal(body, &report); err != nil {
		return mcp.GroupsReport{}, fmt.Errorf("groups: parsing response: %w", err)
	}
	return report, nil
}

// renderGroupsTable prints the groups table, or a configuration hint when
// no groups block exists.
func renderGroupsTable(w io.Writer, report mcp.GroupsReport, plain, verbose bool) {
	if !report.Configured {
		fmt.Fprintln(w, "No tool groups configured. Add a 'groups:' block to stack.yaml, e.g.:")
		fmt.Fprintln(w, "")
		fmt.Fprintln(w, "  groups:")
		fmt.Fprintln(w, "    release:")
		fmt.Fprintln(w, "      servers: [github]")
		fmt.Fprintln(w, "      tools: [gitlab__create_merge_request]")
		fmt.Fprintln(w, "")
		fmt.Fprintln(w, "Then link a client with 'gridctl link <client> --group release'.")
		return
	}

	t := output.NewTableWriter(w, plain)
	t.AppendHeader(table.Row{"Group", "Tools", "Overrides", "Endpoint", "Description"})
	for _, g := range report.Groups {
		t.AppendRow(table.Row{g.Name, g.MemberCount, len(g.Overrides), g.Endpoint, g.Description})
	}
	t.Render()

	if verbose {
		for _, g := range report.Groups {
			fmt.Fprintf(w, "\n## %s\n", g.Name)
			for _, tool := range g.Tools {
				fmt.Fprintf(w, "  %s\n", tool)
			}
		}
	}
}

// warnUnknownGroup checks a group's existence against the running daemon
// before `gridctl link --group` writes a client entry. Best-effort: a down
// or older daemon skips the check silently (the endpoint would 404 at
// connect time anyway, which is its own signal).
func warnUnknownGroup(printer *output.Printer, port int, group string) {
	report, err := fetchGroupsReport(port)
	if err != nil {
		return
	}
	for _, g := range report.Groups {
		if g.Name == group {
			return
		}
	}
	printer.Warn(fmt.Sprintf("group %q is not configured on the running gateway; the linked endpoint will 404 until it exists in stack.yaml", group))
}
