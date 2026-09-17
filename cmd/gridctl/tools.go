package main

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gridctl/gridctl/pkg/output"
	"github.com/spf13/cobra"
)

var (
	toolsStack   string
	toolsAs      string
	toolsTimeout time.Duration
	toolsFormat  string
	toolsJSON    *bool
	toolsLimit   int
)

var toolsCmd = &cobra.Command{
	Use:   "tools",
	Short: "Search live gateway tools",
	Long: `Search tools on a running gateway.

This is lexical substring matching against live names, generated descriptions,
and property names. It is not semantic search. Matching includes the generated
description prefix MCP server: <server>. Call using the exact tool name
"<canonical-name>". Queries such as mcp, server, and call therefore match
every scoped tool in the selected server subset, if any, before limits.

gridctl search remains the install catalog. This command never starts a
daemon or invokes a tool.`,
}

var toolsSearchCmd = &cobra.Command{
	Use:   "search <query>",
	Short: "Search live tools on the running gateway",
	Example: `  gridctl tools search "message" --limit 20 --as automation
  gridctl tools search mcp --format json`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runToolsSearch(cmd, args[0])
	},
}

func init() {
	toolsSearchCmd.Flags().StringVar(&toolsStack, "stack", "", "Stack name when more than one gateway is running")
	toolsSearchCmd.Flags().StringVar(&toolsAs, "as", "", "Caller-declared scope/accounting label (does not authenticate)")
	toolsSearchCmd.Flags().DurationVar(&toolsTimeout, "timeout", 60*time.Second, "Request timeout (must be positive)")
	toolsSearchCmd.Flags().StringVar(&toolsFormat, "format", "", "Output format (json)")
	toolsJSON = addJSONAlias(toolsSearchCmd)
	toolsSearchCmd.Flags().IntVar(&toolsLimit, "limit", 20, "Maximum tools to return (1-200)")
	toolsCmd.AddCommand(toolsSearchCmd)
}

func runToolsSearch(cmd *cobra.Command, query string) error {
	format, err := resolveFormat(toolsFormat, cmd.Flags().Changed("format"), toolsJSON != nil && *toolsJSON)
	if err != nil {
		return invalidCallErr(false, "", "", "input", reasonInvalidFlag, err.Error())
	}
	asJSON := strings.EqualFold(format, "json")
	if toolsTimeout <= 0 {
		return invalidCallErr(asJSON, "", "", "input", reasonInvalidTimeout, "timeout must be a positive duration")
	}
	if toolsLimit < 1 || toolsLimit > 200 {
		return invalidCallErr(asJSON, "", "", "input", reasonInvalidLimit, "limit must be between 1 and 200")
	}
	client, err := normalizeCLIClient(toolsAs, cmd.Flags().Changed("as"))
	if err != nil {
		return invalidCallErr(asJSON, "", "", "input", reasonInvalidIdentity, "invalid --as value")
	}
	port, err := resolveRunningPort("tools", toolsStack)
	if err != nil {
		return daemonUnavailableError(asJSON, err)
	}
	api := newDaemonAPI(port, 0)
	q := url.Values{}
	q.Set("query", query)
	q.Set("client", client)
	q.Set("limit", strconv.Itoa(toolsLimit))
	ctx, cancel := contextTimeout(cmd.Context(), toolsTimeout)
	defer cancel()
	resp, err := api.originDo(ctx, http.MethodGet, "/api/tools/discover?"+q.Encode(), nil, "")
	if err != nil {
		return mapCallTransport(asJSON, client, "", err, ctx)
	}
	body, err := readCappedResponse(resp)
	if err != nil {
		return invalidCallErr(asJSON, client, "", "daemon", reasonResponseTooLarge, "gateway response exceeds 16 MiB")
	}
	if resp.StatusCode != http.StatusOK {
		return classifyHTTPFailure(resp, body, asJSON, client, "")
	}
	result, err := decodeDiscoverEnvelope(body)
	if err != nil {
		return invalidCallErr(asJSON, client, "", "daemon", reasonMalformedResponse, "malformed gateway response")
	}
	if asJSON {
		return output.EncodeJSON(cmd.OutOrStdout(), result)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "%d matched, %d shown (substring match)\n", result.Matched, result.Returned)
	if result.Truncated {
		fmt.Fprintln(cmd.OutOrStdout(), "Results truncated; raise --limit or narrow the query.")
	}
	for _, tool := range result.Tools {
		desc := strings.TrimSpace(tool.Description)
		if desc == "" {
			desc = "(no description)"
		}
		fmt.Fprintf(cmd.OutOrStdout(), "  %s  %s\n", sanitizeTerminal(tool.Name), sanitizeTerminal(truncateHelp(desc, 80)))
	}
	return nil
}
