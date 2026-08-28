package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/gridctl/gridctl/pkg/config"
	"github.com/gridctl/gridctl/pkg/output"
	"github.com/gridctl/gridctl/pkg/state"
	"github.com/gridctl/gridctl/pkg/varrun"
	"github.com/gridctl/gridctl/pkg/varscan"
	"github.com/gridctl/gridctl/pkg/vault"
	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"
)

var (
	varExplainFormat string
	varExplainFile   string
	varExplainJSON   *bool
	varRunSets       []string
	varRunOnly       []string
	varRunAll        bool
	varRunNoRedact   bool
	varRunRedact     bool
	varScanStaged    bool
	varScanFormat    string
	varScanJSON      *bool
)

var varExplainCmd = &cobra.Command{
	Use:   "explain KEY",
	Short: "Explain how a variable resolves without showing its value",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		format, err := resolveFormat(varExplainFormat, cmd.Flags().Changed("format"), *varExplainJSON)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		code, err := runVarExplain(cmd, args[0], format, varExplainFile)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		if code != 0 {
			os.Exit(code)
		}
	},
}

var varRunCmd = &cobra.Command{
	Use:   "run [flags] -- <command> [args...]",
	Short: "Run a command with selected stored variables",
	Args:  cobra.ArbitraryArgs,
	Run: func(cmd *cobra.Command, args []string) {
		if cmd.ArgsLenAtDash() < 0 || len(args) == 0 {
			fmt.Fprintln(os.Stderr, "command is required after --")
			os.Exit(3)
		}
		code, err := runVarCommand(cmd, args)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(3)
		}
		if code != 0 {
			os.Exit(code)
		}
	},
}

var varScanCmd = &cobra.Command{
	Use:   "scan [paths...]",
	Short: "Scan files for exact stored secret values",
	Args:  cobra.ArbitraryArgs,
	Run: func(cmd *cobra.Command, args []string) {
		format, err := resolveFormat(varScanFormat, cmd.Flags().Changed("format"), *varScanJSON)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		code, err := runVarScan(cmd, args, format)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		if code != 0 {
			os.Exit(code)
		}
	},
}

func init() {
	varExplainCmd.Flags().StringVarP(&varExplainFile, "file", "f", "", "Stack file (default: running stack or ./stack.yaml)")
	varExplainCmd.Flags().StringVar(&varExplainFormat, "format", "table", "Output format (table, json)")
	varExplainJSON = addJSONAlias(varExplainCmd)

	varRunCmd.Flags().StringArrayVar(&varRunSets, "set", nil, "Include every variable in a set (repeatable)")
	varRunCmd.Flags().StringSliceVar(&varRunOnly, "only", nil, "Include variable keys (repeatable, comma-separated)")
	varRunCmd.Flags().BoolVar(&varRunAll, "all", false, "Include every stored variable")
	varRunCmd.Flags().BoolVar(&varRunNoRedact, "no-redact", false, "Pass stdout and stderr through without redaction")
	varRunCmd.Flags().BoolVar(&varRunRedact, "redact", false, "Force redaction; invalid when either output is a TTY")

	varScanCmd.Flags().BoolVar(&varScanStaged, "staged", false, "Scan Git index blobs instead of working-tree files")
	varScanCmd.Flags().StringVar(&varScanFormat, "format", "table", "Output format (table, json)")
	varScanJSON = addJSONAlias(varScanCmd)

	varCmd.AddCommand(varExplainCmd, varRunCmd, varScanCmd)
}

type explainDocument struct {
	Key                string                      `json:"key"`
	Locked             bool                        `json:"locked"`
	Stored             *bool                       `json:"stored"`
	Type               *string                     `json:"type"`
	Sensitive          *bool                       `json:"sensitive"`
	Set                *string                     `json:"set"`
	LastRotated        *string                     `json:"last_rotated"`
	EnvironmentPresent bool                        `json:"environment_present"`
	Verdict            *string                     `json:"verdict"`
	Consumers          []config.Consumer           `json:"consumers"`
	ConsumerCoverage   string                      `json:"consumer_coverage"`
	Declaration        *config.VariableDeclaration `json:"declaration,omitempty"`
	Description        *string                     `json:"description"`
	Docs               *string                     `json:"docs"`
	Example            *string                     `json:"example"`
	Deprecated         *string                     `json:"deprecated"`
	Problems           []string                    `json:"problems"`
}

func runVarExplain(cmd *cobra.Command, key, format, stackFile string) (int, error) {
	store, err := loadVault()
	if err != nil {
		return 2, err
	}
	_, envPresent := os.LookupEnv(key)
	doc := explainDocument{Key: key, Locked: store.IsLocked(), EnvironmentPresent: envPresent, Consumers: []config.Consumer{}, ConsumerCoverage: "complete", Problems: []string{}}

	if vault.IsInternalCredential(key) {
		problem := vault.NewInternalCredentialError(key).Error()
		verdict := "denied"
		doc.Verdict, doc.Problems = &verdict, []string{problem}
	} else if store.IsLocked() {
		doc.ConsumerCoverage = "partial"
		doc.Problems = append(doc.Problems, "store is locked; stored metadata and resolution are unknown")
	} else {
		v, stored := store.GetVariable(key)
		doc.Stored = &stored
		result := config.ResolveVariable(store, key)
		verdict := strings.ReplaceAll(string(result.Verdict), "_", " ")
		doc.Verdict = &verdict
		if stored {
			typeName, sensitive, set, rotated := string(v.Type), v.IsSecret, v.Set, v.LastRotated
			description, docs, example, deprecated := v.Description, v.Docs, v.Example, v.Deprecated
			doc.Type, doc.Sensitive, doc.Set, doc.LastRotated = &typeName, &sensitive, &set, &rotated
			doc.Description, doc.Docs, doc.Example, doc.Deprecated = &description, &docs, &example, &deprecated
		}
		if result.Verdict == config.ResolutionUnset {
			doc.Problems = append(doc.Problems, "variable is unset")
		}
	}

	stack, usage, complete, err := explainUsage(cmd, stackFile, store)
	if err != nil {
		return 2, err
	}
	doc.Consumers = append(doc.Consumers, usage[key]...)
	sort.Slice(doc.Consumers, func(i, j int) bool {
		a, b := doc.Consumers[i], doc.Consumers[j]
		return fmt.Sprintf("%s/%s/%s/%s", a.Kind, a.Name, a.Field, a.Source) < fmt.Sprintf("%s/%s/%s/%s", b.Kind, b.Name, b.Field, b.Source)
	})
	if !complete {
		doc.ConsumerCoverage = "partial"
	}
	if stack != nil {
		if declaration, ok := stack.Variables[key]; ok {
			doc.Declaration = &declaration
			if declaration.IsRequired() && doc.Verdict != nil && *doc.Verdict == "unset" {
				doc.Problems = append(doc.Problems, "required variable is unavailable")
			}
		}
	}
	if format == "json" {
		return explainExit(doc), writeJSONOutput(cmd.OutOrStdout(), doc)
	}
	renderExplainTable(cmd, doc)
	return explainExit(doc), nil
}

func explainExit(doc explainDocument) int {
	if doc.Locked || len(doc.Problems) > 0 {
		return 1
	}
	return 0
}

func explainUsage(cmd *cobra.Command, stackFile string, store *vault.Store) (*config.Stack, config.ReferenceIndex, bool, error) {
	path, _, err := resolveStackFileTarget(stackFile)
	if err != nil {
		if stackFile != "" {
			return nil, nil, false, err
		}
		return nil, config.ReferenceIndex{}, true, nil
	}
	abs, _ := filepath.Abs(path)
	states, _ := state.List()
	for _, st := range states {
		statePath, _ := filepath.Abs(st.StackFile)
		if state.IsRunning(&st) && statePath == abs {
			api := newDaemonAPIFor(st, 2*time.Second)
			resp, requestErr := api.Get(api.URL("/api/var/usage"))
			if requestErr == nil {
				defer resp.Body.Close()
				if resp.StatusCode == http.StatusOK {
					usage := config.ReferenceIndex{}
					if err := json.NewDecoder(resp.Body).Decode(&usage); err == nil {
						stack, _ := config.ParseStackIndex(cmd.Context(), path)
						return stack, usage, !store.IsLocked(), nil
					}
				}
			}
		}
	}
	stack, err := config.ParseStackIndex(cmd.Context(), path)
	if err != nil {
		return nil, nil, false, err
	}
	var members map[string][]string
	if !store.IsLocked() {
		members = map[string][]string{}
		for _, v := range store.List() {
			if v.Set != "" {
				members[v.Set] = append(members[v.Set], v.Key)
			}
		}
	}
	usage, complete := config.BuildVariableUsage(stack, members)
	return stack, usage, complete, nil
}

func renderExplainTable(cmd *cobra.Command, doc explainDocument) {
	t := output.NewTableWriter(cmd.OutOrStdout(), false)
	t.AppendHeader(table.Row{"Field", "Value"})
	t.AppendRow(table.Row{"Key", doc.Key})
	t.AppendRow(table.Row{"Locked", doc.Locked})
	t.AppendRow(table.Row{"Stored", pointerText(doc.Stored)})
	t.AppendRow(table.Row{"Type", pointerText(doc.Type)})
	t.AppendRow(table.Row{"Sensitive", pointerText(doc.Sensitive)})
	t.AppendRow(table.Row{"Set", pointerText(doc.Set)})
	t.AppendRow(table.Row{"Last rotated", pointerText(doc.LastRotated)})
	t.AppendRow(table.Row{"Environment present", doc.EnvironmentPresent})
	t.AppendRow(table.Row{"Verdict", pointerText(doc.Verdict)})
	t.AppendRow(table.Row{"Consumer coverage", doc.ConsumerCoverage})
	for _, consumer := range doc.Consumers {
		name := fmt.Sprintf("%s/%s", consumer.Kind, consumer.Name)
		if consumer.Name == "" {
			name = string(consumer.Kind)
		}
		t.AppendRow(table.Row{"Consumer", fmt.Sprintf("%s (%s)", name, consumer.Field)})
	}
	if doc.Declaration != nil {
		t.AppendRow(table.Row{"Declared required", doc.Declaration.IsRequired()})
		t.AppendRow(table.Row{"Declared type", doc.Declaration.ValueType()})
		t.AppendRow(table.Row{"Declared sensitive", doc.Declaration.IsSecret()})
		if doc.Declaration.Description != "" {
			t.AppendRow(table.Row{"Declared description", doc.Declaration.Description})
		}
		if doc.Declaration.Docs != "" {
			t.AppendRow(table.Row{"Declared docs", doc.Declaration.Docs})
		}
	}
	if doc.Description != nil && *doc.Description != "" {
		t.AppendRow(table.Row{"Description", *doc.Description})
	}
	if doc.Docs != nil && *doc.Docs != "" {
		t.AppendRow(table.Row{"Docs", *doc.Docs})
	}
	if doc.Deprecated != nil && *doc.Deprecated != "" {
		t.AppendRow(table.Row{"Deprecated", *doc.Deprecated})
	}
	for _, problem := range doc.Problems {
		t.AppendRow(table.Row{"Problem", problem})
	}
	t.Render()
}

func pointerText[T any](value *T) any {
	if value == nil {
		return "unknown"
	}
	return *value
}

func runVarCommand(cmd *cobra.Command, command []string) (int, error) {
	if varRunAll && (len(varRunSets) > 0 || len(varRunOnly) > 0) {
		return 3, fmt.Errorf("--all is mutually exclusive with --set and --only")
	}
	if !varRunAll && len(varRunSets) == 0 && len(varRunOnly) == 0 {
		return 3, fmt.Errorf("select variables with --set, --only, or --all")
	}
	if varRunNoRedact && varRunRedact {
		return 3, fmt.Errorf("--redact and --no-redact are mutually exclusive")
	}
	store, err := loadVault()
	if err != nil {
		return 3, err
	}
	if err := ensureUnlocked(store); err != nil {
		return 3, err
	}
	selected, err := selectRunVariables(store.List())
	if err != nil {
		return 3, err
	}
	result, err := varrun.Run(cmd.Context(), varrun.Options{
		Command: command, Variables: selected, Environment: os.Environ(),
		Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr,
		StdoutTTY: isTerminalFile(os.Stdout), StderrTTY: isTerminalFile(os.Stderr),
		NoRedact: varRunNoRedact, ForceRedact: varRunRedact,
	})
	if err != nil {
		return 3, err
	}
	return result.ExitCode, nil
}

func selectRunVariables(all []vault.Variable) ([]vault.Variable, error) {
	wanted := map[string]bool{}
	sets := map[string]bool{}
	for _, name := range varRunSets {
		sets[name] = true
	}
	for _, key := range varRunOnly {
		if key = strings.TrimSpace(key); key != "" {
			wanted[key] = true
		}
	}
	foundSets := map[string]bool{}
	selected := make([]vault.Variable, 0, len(all))
	for _, v := range all {
		if sets[v.Set] {
			foundSets[v.Set] = true
		}
		if varRunAll || wanted[v.Key] || sets[v.Set] {
			if !vault.IsInternalCredential(v.Key) {
				selected = append(selected, v)
			}
			delete(wanted, v.Key)
		}
	}
	for name := range sets {
		if !foundSets[name] {
			return nil, fmt.Errorf("variable set %q not found or empty", name)
		}
	}
	if len(wanted) > 0 {
		keys := make([]string, 0, len(wanted))
		for key := range wanted {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		return nil, fmt.Errorf("variable(s) not found: %s", strings.Join(keys, ", "))
	}
	return selected, nil
}

func isTerminalFile(f *os.File) bool {
	return isatty.IsTerminal(f.Fd()) || isatty.IsCygwinTerminal(f.Fd())
}

func runVarScan(cmd *cobra.Command, paths []string, format string) (int, error) {
	if varScanStaged && len(paths) > 0 {
		return 2, fmt.Errorf("paths cannot be combined with --staged")
	}
	store, err := loadVault()
	if err != nil {
		return 2, err
	}
	if store.IsLocked() {
		return 2, fmt.Errorf("variable store is locked; unlock it before scanning")
	}
	result, err := varscan.Scan(cmd.Context(), store.List(), varscan.Options{Paths: paths, Staged: varScanStaged})
	if err != nil {
		return 2, err
	}
	if format == "json" {
		if err := writeJSONOutput(cmd.OutOrStdout(), result); err != nil {
			return 2, err
		}
	} else {
		renderScanTable(cmd, result)
	}
	if !result.Complete {
		return 2, nil
	}
	if len(result.Findings) > 0 {
		return 1, nil
	}
	return 0, nil
}

func renderScanTable(cmd *cobra.Command, result varscan.Result) {
	t := output.NewTableWriter(cmd.OutOrStdout(), false)
	t.AppendHeader(table.Row{"File", "Line", "Column", "Key", "Code", "Severity", "Snippet"})
	for _, finding := range result.Findings {
		t.AppendRow(table.Row{finding.File, finding.Line, finding.Column, finding.Key, finding.Code, finding.Severity, finding.Snippet})
	}
	t.Render()
	fmt.Fprintf(cmd.OutOrStdout(), "Findings: %d; skipped: %d; truncated files: %d\n", len(result.Findings), len(result.Skipped), len(result.Truncations))
}

func writeJSONOutput(w interface{ Write([]byte) (int, error) }, value any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(value)
}
