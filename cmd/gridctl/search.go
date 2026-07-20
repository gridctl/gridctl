package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/gridctl/gridctl/pkg/catalog"
	"github.com/gridctl/gridctl/pkg/output"

	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/spf13/cobra"
)

const searchExitInfrastructure = 2

// searchJSONSchemaVersion identifies the shape of the search JSON document.
const searchJSONSchemaVersion = 1

var (
	searchSource string
	searchFormat string
	searchAsJSON *bool
	searchPlain  *bool
)

// searchRegistry queries the MCP Registry; a seam so tests run without a
// network. Returns entries, whether they came from a stale cache, and the
// fetch error when neither network nor cache could answer.
var searchRegistry = func(ctx context.Context, query string) ([]catalog.Entry, bool, error) {
	return catalog.NewClient().Search(ctx, query)
}

var searchCmd = &cobra.Command{
	Use:   "search [query]",
	Short: "Search the MCP server catalog",
	Long: `Searches the server catalog: a curated set embedded in gridctl plus the
official MCP Registry (registry.modelcontextprotocol.io). Install a result
by name with 'gridctl add <name>'.

Without a query, the curated catalog is listed and the registry is not
contacted. Registry responses are cached for an hour under
~/.gridctl/cache/catalog; when the registry is unreachable, cached or
curated results are shown with a warning. Registry entries are community
publications, not vetted by gridctl.

This command searches the install catalog. The 'search' meta-tool that
code-mode gateways expose to LLM clients searches the running gateway's
tools and is unrelated.

Exit codes:
  0  success (including no matches)
  2  infrastructure error (invalid flags, corrupt embedded catalog)`,
	Example: `  gridctl search                    List the curated catalog
  gridctl search postgres           Search curated and registry entries
  gridctl search github --json      Machine-readable results
  gridctl search --source curated   Curated entries only`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		format, err := resolveFormat(searchFormat, cmd.Flags().Changed("format"), *searchAsJSON)
		if err == nil {
			err = resolvePlain(*searchPlain, format)
		}
		if err == nil && searchSource != "curated" && searchSource != "registry" && searchSource != "all" {
			err = fmt.Errorf("invalid --source %q (allowed: curated, registry, all)", searchSource)
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(searchExitInfrastructure)
		}
		query := ""
		if len(args) == 1 {
			query = args[0]
		}
		return runSearch(cmd.Context(), query, format, searchSource, *searchPlain)
	},
}

func init() {
	searchCmd.Flags().StringVar(&searchSource, "source", "all", "Catalog sources to search: curated, registry, or all")
	searchCmd.Flags().StringVar(&searchFormat, "format", "", "Output format: 'json' for machine-readable output (default: table)")
	searchAsJSON = addJSONAlias(searchCmd)
	searchPlain = addPlainFlag(searchCmd)
}

// --- JSON document ---

type searchServerDoc struct {
	Name        string `json:"name"`
	Title       string `json:"title,omitempty"`
	Description string `json:"description"`
	Tier        string `json:"tier"`
	Transport   string `json:"transport,omitempty"`
	Status      string `json:"status"`
	Homepage    string `json:"homepage,omitempty"`
	Repository  string `json:"repository,omitempty"`
	Unsupported string `json:"unsupported,omitempty"`
}

type searchDoc struct {
	SchemaVersion int               `json:"schema_version"`
	Query         string            `json:"query"`
	Source        string            `json:"source"`
	Stale         bool              `json:"stale,omitempty"`
	RegistryError string            `json:"registry_error,omitempty"`
	Servers       []searchServerDoc `json:"servers"`
}

func runSearch(ctx context.Context, query, format, source string, plain bool) error {
	// In JSON mode stdout carries exactly one document; narration moves to
	// stderr so pipelines can parse the output.
	printer := output.New()
	jsonMode := strings.EqualFold(format, "json")
	if jsonMode {
		printer = output.NewWithWriter(os.Stderr)
	}

	var curated []catalog.Entry
	if source != "registry" {
		var err error
		curated, err = catalog.FilterCurated(query)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(searchExitInfrastructure)
		}
	}

	var registryEntries []catalog.Entry
	doc := searchDoc{SchemaVersion: searchJSONSchemaVersion, Query: query, Source: source}
	// The registry is a substring search; without a query it would page
	// through the whole registry for no benefit, so the empty query stays
	// curated-only by design.
	if source != "curated" && query != "" {
		entries, stale, err := searchRegistry(ctx, query)
		switch {
		case err != nil:
			doc.RegistryError = err.Error()
			fallback := "showing curated results only"
			if source == "registry" {
				fallback = "no results"
			}
			printer.Warn(fmt.Sprintf("MCP Registry unavailable (%v); %s", err, fallback))
		case stale:
			doc.Stale = true
			printer.Warn("MCP Registry unavailable; showing cached registry results (may be stale)")
			registryEntries = entries
		default:
			registryEntries = entries
		}
	}

	merged := catalog.Merge(curated, registryEntries)
	for _, e := range merged {
		doc.Servers = append(doc.Servers, searchServerDoc{
			Name: e.Name, Title: e.Title, Description: e.Description,
			Tier: e.Tier, Transport: e.Install.Transport, Status: e.Status,
			Homepage: e.Homepage, Repository: e.Repository, Unsupported: e.Unsupported,
		})
	}

	if jsonMode {
		if err := output.EncodeJSON(os.Stdout, doc); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(searchExitInfrastructure)
		}
		return nil
	}

	if len(merged) == 0 {
		if query == "" {
			printer.Info("Pass a query to search the registry, e.g. 'gridctl search postgres'")
			return nil
		}
		printer.Info(fmt.Sprintf("No servers matched %q", query))
		return nil
	}

	renderSearchTable(merged, plain)
	printer.Print("\nRun 'gridctl add <name>' to add a server to the stack.\n")
	return nil
}

// renderSearchTable prints the merged results. The SOURCE cell carries the
// deprecation marker so the table stays four columns wide.
func renderSearchTable(entries []catalog.Entry, plain bool) {
	t := output.NewTableWriter(os.Stdout, plain)
	t.AppendHeader(table.Row{"Name", "Description", "Source", "Transport"})
	for _, e := range entries {
		source := e.Tier
		if e.Status == catalog.StatusDeprecated {
			source += " (deprecated)"
		}
		transport := e.Install.Transport
		if e.Unsupported != "" {
			transport = "unsupported"
		}
		t.AppendRow(table.Row{e.Name, truncate(e.Description, 60), source, transport})
	}
	t.Render()
}

// truncate shortens s to max runes with an ellipsis.
func truncate(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max-1]) + "…"
}
