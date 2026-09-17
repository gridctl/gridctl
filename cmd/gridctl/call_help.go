package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/gridctl/gridctl/pkg/mcp"
	"github.com/gridctl/gridctl/pkg/output"
	"github.com/spf13/cobra"
)

func runCallHelp(cmd *cobra.Command, args []string) error {
	if err := applyGlobalRuntimeSettings(); err != nil {
		asJSON, _ := callJSONMode(cmd)
		return invalidCallErr(asJSON, "", "", "input", reasonInvalidFlag, err.Error())
	}
	asJSON, err := callJSONMode(cmd)
	if err != nil {
		return invalidCallErr(false, "", "", "input", reasonInvalidFlag, err.Error())
	}
	if callTimeout <= 0 {
		return invalidCallErr(asJSON, "", "", "input", reasonInvalidTimeout, "timeout must be a positive duration")
	}
	target := args[0]
	client, err := normalizeCLIClient(callAs, cmd.Flags().Changed("as"))
	if err != nil {
		return invalidCallErr(asJSON, "", "", "input", reasonInvalidIdentity, "invalid --as value")
	}
	if strings.Contains(target, mcp.ToolNameDelimiter) {
		if cmd.Flags().Changed("match") || cmd.Flags().Changed("limit") {
			return invalidCallErr(asJSON, client, target, "input", reasonInvalidFlag, "--match and --limit are not valid for leaf help")
		}
		if _, err := parseCallTarget(target); err != nil {
			return invalidCallErr(asJSON, client, "", "input", reasonInvalidTarget, "target must be server__tool")
		}
		return liveLeafHelp(cmd, asJSON, client, target)
	}
	if callLimit < 1 || callLimit > 200 {
		return invalidCallErr(asJSON, client, "", "input", reasonInvalidLimit, "limit must be between 1 and 200")
	}
	return liveServerHelp(cmd, asJSON, client, target)
}

func applyGlobalRuntimeSettings() error {
	return rootCmd.PersistentPreRunE(rootCmd, nil)
}

func liveLeafHelp(cmd *cobra.Command, asJSON bool, client, name string) error {
	q := url.Values{}
	q.Set("name", name)
	q.Set("client", client)
	result, err := discoverRequest(cmd.Context(), asJSON, client, name, q)
	if err != nil {
		return err
	}
	if asJSON {
		return output.EncodeJSON(cmd.OutOrStdout(), result)
	}
	if len(result.Tools) == 0 {
		return invalidCallErr(false, client, name, "routing", "not_found", "tool not found")
	}
	renderLeafHelp(cmd, result.Tools[0])
	return nil
}

func liveServerHelp(cmd *cobra.Command, asJSON bool, client, server string) error {
	q := url.Values{}
	q.Set("server", server)
	q.Set("client", client)
	if callMatch != "" {
		q.Set("query", callMatch)
	}
	q.Set("limit", strconv.Itoa(callLimit))
	result, err := discoverRequest(cmd.Context(), asJSON, client, "", q)
	if err != nil {
		return err
	}
	if asJSON {
		return output.EncodeJSON(cmd.OutOrStdout(), result)
	}
	renderServerHelp(cmd, server, result)
	return nil
}

func discoverRequest(ctx context.Context, asJSON bool, client, name string, q url.Values) (mcp.ToolDiscoverResult, error) {
	port, err := resolveRunningPort("call", callStack)
	if err != nil {
		return mcp.ToolDiscoverResult{}, daemonUnavailableError(asJSON, err)
	}
	api := newDaemonAPI(port, 0)
	ctx, cancel := contextTimeout(ctx, callTimeout)
	defer cancel()
	resp, err := api.originDo(ctx, http.MethodGet, "/api/tools/discover?"+q.Encode(), nil, "")
	if err != nil {
		return mcp.ToolDiscoverResult{}, mapCallTransport(asJSON, client, name, err, ctx)
	}
	body, err := readCappedResponse(resp)
	if err != nil {
		return mcp.ToolDiscoverResult{}, invalidCallErr(asJSON, client, name, "daemon", reasonResponseTooLarge, "gateway response exceeds 16 MiB")
	}
	if resp.StatusCode != http.StatusOK {
		return mcp.ToolDiscoverResult{}, classifyHTTPFailure(resp, body, asJSON, client, name)
	}
	result, err := decodeDiscoverEnvelope(body)
	if err != nil {
		return mcp.ToolDiscoverResult{}, invalidCallErr(asJSON, client, name, "daemon", reasonMalformedResponse, "malformed gateway response")
	}
	return result, nil
}

func renderServerHelp(cmd *cobra.Command, server string, result mcp.ToolDiscoverResult) {
	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "Server %s: %d visible, %d matched, %d shown\n", sanitizeTerminal(server), result.TotalVisible, result.Matched, result.Returned)
	if result.Truncated {
		fmt.Fprintln(out, "Results truncated; raise --limit or narrow --match.")
	}
	for _, tool := range result.Tools {
		desc := strings.TrimSpace(tool.Description)
		if desc == "" {
			desc = "(no description)"
		}
		fmt.Fprintf(out, "  %s  %s\n", sanitizeTerminal(tool.Name), sanitizeTerminal(truncateHelp(desc, 80)))
	}
}

func renderLeafHelp(cmd *cobra.Command, tool mcp.Tool) {
	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "Name: %s\n", sanitizeTerminal(tool.Name))
	fmt.Fprintf(out, "Description: %s\n", sanitizeTerminal(tool.Description))
	fmt.Fprintln(out, "Input schema:")
	if len(tool.InputSchema) > 0 {
		fmt.Fprintln(out, string(tool.InputSchema))
	}
	props, partial := summarizeSchema(tool.InputSchema)
	fmt.Fprintln(out, "Properties:")
	if len(props) == 0 {
		fmt.Fprintln(out, "  (none)")
	}
	for _, p := range props {
		req := "optional"
		if p.required {
			req = "required"
		}
		line := fmt.Sprintf("  %s (%s", p.name, req)
		if p.typ != "" {
			line += ", " + p.typ
		}
		line += ")"
		if p.description != "" {
			line += " " + p.description
		}
		if len(p.enum) > 0 {
			line += " enum=" + strings.Join(p.enum, ",")
		}
		if p.hasDefault {
			line += fmt.Sprintf(" default=%v", p.def)
		}
		fmt.Fprintln(out, sanitizeTerminal(line))
	}
	if partial {
		fmt.Fprintln(out, "Property summary is partial; remote references were not resolved.")
	}
}

type schemaProp struct {
	name        string
	required    bool
	typ         string
	description string
	enum        []string
	def         any
	hasDefault  bool
}

func summarizeSchema(raw []byte) ([]schemaProp, bool) {
	if len(raw) == 0 {
		return nil, false
	}
	var schema struct {
		Properties map[string]map[string]any `json:"properties"`
		Required   []string                  `json:"required"`
		Ref        string                    `json:"$ref"`
	}
	if err := jsonUnmarshal(raw, &schema); err != nil {
		return nil, true
	}
	partial := schema.Ref != ""
	req := map[string]bool{}
	for _, n := range schema.Required {
		req[n] = true
	}
	names := make([]string, 0, len(schema.Properties))
	for n, spec := range schema.Properties {
		names = append(names, n)
		if spec != nil {
			if _, ok := spec["$ref"]; ok {
				partial = true
			}
		}
	}
	sort.Strings(names)
	out := make([]schemaProp, 0, len(names))
	for _, n := range names {
		spec := schema.Properties[n]
		p := schemaProp{name: n, required: req[n]}
		if spec != nil {
			if t, ok := spec["type"].(string); ok {
				p.typ = t
			}
			if d, ok := spec["description"].(string); ok {
				p.description = d
			}
			if e, ok := spec["enum"].([]any); ok {
				for _, v := range e {
					if s, ok := v.(string); ok {
						p.enum = append(p.enum, s)
					} else {
						partial = true
					}
				}
			}
			if def, ok := spec["default"]; ok {
				p.def = def
				p.hasDefault = true
			}
			if p.typ == "" && spec["type"] != nil {
				partial = true
			}
		}
		out = append(out, p)
	}
	return out, partial
}

func jsonUnmarshal(raw []byte, v any) error {
	return json.Unmarshal(raw, v)
}

func truncateHelp(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-3] + "..."
}
