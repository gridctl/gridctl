package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/gridctl/gridctl/pkg/ctxpins"
	"github.com/gridctl/gridctl/pkg/output"
	"github.com/gridctl/gridctl/pkg/pinapprovals"
	"github.com/gridctl/gridctl/pkg/pins"

	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/spf13/cobra"
)

var (
	ctxPinsListFormat      string
	ctxPinsListJSON        *bool
	ctxPinsListPlain       *bool
	ctxPinsVerifyFormat    string
	ctxPinsVerifyJSON      *bool
	ctxPinsVerifyFailOn    string
	ctxPinsDiffFormat      string
	ctxPinsDiffJSON        *bool
	ctxPinsDiffFailOn      string
	ctxPinsApproveExpect   string
	ctxPinsApproveReason   string
	ctxPinsApproveEvidence []string
)

var ctxPinsCmd = &cobra.Command{
	Use:   "pins",
	Short: "Manage content pins for context fragments",
	Long: `Inspect, verify, approve, and reset TOFU content pins for global context.

Context pins record a digest of each rule fragment, or of the canonical
AGENTS.md when fragments mode is off. Content changes surface as "pin
drift" that persists until approved or reset. This is the stackless
analogue of 'gridctl skill pins'. Projection warns and continues; it does
not refuse on pin drift.

Not to be confused with client drift (a hand-edited projection) or
staleness (the canon moved since the last sync).`,
}

var ctxPinsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List pin status for context fragments",
	Long: `List pin status for every pinned fragment.

Default output is a styled table. Use '--format json' for machine-readable
output including the pinned document, provenance, and advisory findings.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		format, err := resolveFormat(ctxPinsListFormat, cmd.Flags().Changed("format"), *ctxPinsListJSON)
		if err != nil {
			return err
		}
		if err := resolvePlain(*ctxPinsListPlain, format); err != nil {
			return err
		}
		return runCtxPinsList(cmd.Context(), os.Stdout, os.Stderr, format)
	},
}

var ctxPinsVerifyCmd = &cobra.Command{
	Use:   "verify [fragment]",
	Short: "Verify fragment content against pins",
	Long: `Recompute every pinned fragment's digest and report drift.

Exit codes:
  0  all pins verified (or nothing pinned yet)
  1  pin drift detected, or scan findings at or above the
     --fail-on-findings severity ('warn' or 'critical')
  2  infrastructure error (home unresolvable, unreadable or newer pin
     file, unknown fragment, not pinned)`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		format, err := resolveFormat(ctxPinsVerifyFormat, cmd.Flags().Changed("format"), *ctxPinsVerifyJSON)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(pinsExitInfrastructure)
		}
		name := ""
		if len(args) == 1 {
			name = args[0]
		}
		if err := validateFailOnFindings(ctxPinsVerifyFailOn); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(pinsExitInfrastructure)
		}
		exit := runCtxPinsVerify(cmd.Context(), os.Stdout, os.Stderr, name, format, ctxPinsVerifyFailOn)
		if exit != pinsExitOK {
			os.Exit(exit)
		}
		return nil
	},
}

var ctxPinsDiffCmd = &cobra.Command{
	Use:   "diff <fragment>",
	Short: "Show what changed for a drifted fragment",
	Long: `Show how a fragment's document moved since its pin, plus any advisory
findings on the new content.

The JSON output includes the pinned and current documents plus the
composite_hash for 'ctx pins approve --expect'. Findings are advisory and
never affect the exit code unless --fail-on-findings is passed.

Exit codes:
  0  no pin drift
  1  pin drift detected, or scan findings at or above --fail-on-findings
  2  infrastructure error (home unresolvable, fragment not found or not
     pinned, content unreadable, newer pin file)`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		format, err := resolveFormat(ctxPinsDiffFormat, cmd.Flags().Changed("format"), *ctxPinsDiffJSON)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(pinsExitInfrastructure)
		}
		if err := validateFailOnFindings(ctxPinsDiffFailOn); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(pinsExitInfrastructure)
		}
		exit := runCtxPinsDiff(cmd.Context(), os.Stdout, os.Stderr, args[0], format, ctxPinsDiffFailOn)
		if exit != pinsExitOK {
			os.Exit(exit)
		}
		return nil
	},
}

var ctxPinsApproveCmd = &cobra.Command{
	Use:   "approve <fragment>",
	Short: "Approve content changes for a fragment",
	Long: `Re-pin a fragment's current content, clearing pin drift.

Pass --expect with the composite_hash from 'ctx pins diff --format json'
to bind the approval to the reviewed content. --reason is required when
the current content carries unresolved advisory findings, and is recorded
on every approval when supplied. Repeat --evidence with an opaque
reference. References are stored as text and never fetched.
Credential-shaped values are refused.

The approval is recorded with the local OS user. approved_by is
attribution, not authentication.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runCtxPinsApprove(cmd.Context(), os.Stdout, os.Stderr, args[0], ctxPinsApproveExpect, ctxPinsApproveReason, ctxPinsApproveEvidence)
	},
}

var ctxPinsResetCmd = &cobra.Command{
	Use:   "reset <fragment>",
	Short: "Delete the pin for a fragment",
	Long:  "Remove a fragment's pin record. It is re-pinned on the next sync. Approval history is kept.",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if code := runCtxPinsReset(cmd.Context(), os.Stdout, os.Stderr, args[0]); code != pinsExitOK {
			os.Exit(code)
		}
		return nil
	},
}

func init() {
	ctxPinsListCmd.Flags().StringVar(&ctxPinsListFormat, "format", "", "Output format: 'json' for machine-readable output (default: table)")
	ctxPinsListJSON = addJSONAlias(ctxPinsListCmd)
	ctxPinsListPlain = addPlainFlag(ctxPinsListCmd)

	ctxPinsVerifyCmd.Flags().StringVar(&ctxPinsVerifyFormat, "format", "", "Output format: 'json' for machine-readable output (default: text)")
	ctxPinsVerifyJSON = addJSONAlias(ctxPinsVerifyCmd)
	ctxPinsVerifyCmd.Flags().StringVar(&ctxPinsVerifyFailOn, "fail-on-findings", "",
		"Exit 1 when scan findings at or above this severity exist: 'warn' or 'critical' (default: findings never affect the exit code)")

	ctxPinsDiffCmd.Flags().StringVar(&ctxPinsDiffFormat, "format", "", "Output format: 'json' for machine-readable output (default: text)")
	ctxPinsDiffJSON = addJSONAlias(ctxPinsDiffCmd)
	ctxPinsDiffCmd.Flags().StringVar(&ctxPinsDiffFailOn, "fail-on-findings", "",
		"Exit 1 when scan findings at or above this severity exist: 'warn' or 'critical' (default: findings never affect the exit code)")

	ctxPinsApproveCmd.Flags().StringVar(&ctxPinsApproveExpect, "expect", "", "Reviewed composite_hash from 'ctx pins diff'; approval is rejected if the content no longer matches")
	ctxPinsApproveCmd.Flags().StringVar(&ctxPinsApproveReason, "reason", "", "Reason for this approval (required when unresolved advisory findings are present)")
	ctxPinsApproveCmd.Flags().StringArrayVar(&ctxPinsApproveEvidence, "evidence", nil, "Opaque evidence reference (repeatable). Stored as text and never fetched")

	ctxPinsCmd.AddCommand(ctxPinsListCmd)
	ctxPinsCmd.AddCommand(ctxPinsVerifyCmd)
	ctxPinsCmd.AddCommand(ctxPinsDiffCmd)
	ctxPinsCmd.AddCommand(ctxPinsApproveCmd)
	ctxPinsCmd.AddCommand(ctxPinsResetCmd)
	ctxCmd.AddCommand(ctxPinsCmd)
}

type ctxPinsListDoc struct {
	SchemaVersion int                             `json:"schema_version"`
	Fragments     map[string]*ctxpins.FragmentPin `json:"fragments"`
}

func runCtxPinsList(ctx context.Context, stdout, stderr io.Writer, format string) error {
	store, _, err := loadCtxPins(ctx)
	if err != nil {
		return err
	}
	fragments, err := store.GetAll(ctx)
	if err != nil {
		return err
	}
	if fragments == nil {
		fragments = map[string]*ctxpins.FragmentPin{}
	}
	if strings.EqualFold(format, "json") {
		return output.EncodeJSON(stdout, ctxPinsListDoc{
			SchemaVersion: ctxJSONSchemaVersion,
			Fragments:     fragments,
		})
	}
	if len(fragments) == 0 {
		fmt.Fprintln(stdout, "No fragment pins yet. Run 'gridctl ctx sync' to pin the context store.")
		return nil
	}
	t := output.NewTableWriter(stdout, *ctxPinsListPlain)
	t.AppendHeader(table.Row{"FRAGMENT", "STATUS", "SOURCE", "FINDINGS", "LAST VERIFIED"})
	for _, name := range sortedMapKeys(fragments) {
		pin := fragments[name]
		source := pin.Source
		if pin.Pack != "" {
			source = pin.Pack
		}
		t.AppendRow(table.Row{
			name,
			pinStatusLabel(pin.Status),
			source,
			len(pin.Findings),
			pin.LastVerifiedAt.Format("2006-01-02 15:04:05"),
		})
	}
	t.Render()
	return nil
}

type ctxPinsVerifyFragment struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Source string `json:"source,omitempty"`
}

type ctxPinsVerifyDoc struct {
	SchemaVersion int                     `json:"schema_version"`
	HasDrift      bool                    `json:"has_drift"`
	Fragments     []ctxPinsVerifyFragment `json:"fragments"`
	Missing       []string                `json:"missing"`
}

func runCtxPinsVerify(ctx context.Context, stdout, stderr io.Writer, name, format, failOn string) int {
	store, src, err := loadCtxPins(ctx)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return pinsExitInfrastructure
	}
	pinned, err := store.GetAll(ctx)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return pinsExitInfrastructure
	}
	doc := ctxPinsVerifyDoc{
		SchemaVersion: ctxJSONSchemaVersion,
		Fragments:     []ctxPinsVerifyFragment{},
		Missing:       []string{},
	}
	names := sortedMapKeys(pinned)
	if name != "" {
		if _, ok := pinned[name]; !ok {
			fmt.Fprintf(stderr, "no pin found for fragment %q (see 'gridctl ctx pins list')\n", name)
			return pinsExitInfrastructure
		}
		names = []string{name}
	}
	for _, frag := range names {
		vr, verr := store.Verify(ctx, frag, src)
		switch {
		case errors.Is(verr, ctxpins.ErrDigestUnavailable):
			fmt.Fprintf(stderr, "  ✗ %-24s content could not be hashed; treating as pin drift (%v)\n", frag, verr)
			doc.Fragments = append(doc.Fragments, ctxPinsVerifyFragment{Name: frag, Status: ctxpins.StatusDrift, Source: pinned[frag].Source})
			doc.HasDrift = true
			continue
		case errors.Is(verr, ctxpins.ErrNoDocument):
			doc.Missing = append(doc.Missing, frag)
			continue
		case verr != nil:
			fmt.Fprintln(stderr, verr)
			return pinsExitInfrastructure
		}
		doc.Fragments = append(doc.Fragments, ctxPinsVerifyFragment{Name: frag, Status: vr.Status, Source: pinned[frag].Source})
		if vr.Status == ctxpins.StatusDrift {
			doc.HasDrift = true
		}
	}
	if strings.EqualFold(format, "json") {
		if err := output.EncodeJSON(stdout, doc); err != nil {
			fmt.Fprintln(stderr, err)
			return pinsExitInfrastructure
		}
	} else {
		if len(doc.Fragments) == 0 && len(doc.Missing) == 0 {
			fmt.Fprintln(stdout, "No fragment pins yet. Run 'gridctl ctx sync' to pin the context store.")
			return pinsExitOK
		}
		for _, fv := range doc.Fragments {
			if fv.Status == ctxpins.StatusDrift {
				fmt.Fprintf(stdout, "  ✗ %-24s pin drift detected\n", fv.Name)
			} else {
				fmt.Fprintf(stdout, "  ✓ %-24s verified\n", fv.Name)
			}
		}
		for _, missing := range doc.Missing {
			fmt.Fprintf(stdout, "  ? %-24s pinned but missing from the context store (run 'gridctl ctx pins reset %s' to drop the record)\n", missing, missing)
		}
	}
	if doc.HasDrift {
		return pinsExitDrift
	}
	if failOn != "" && fragmentFindingsAtOrAbove(pinned, names, failOn) {
		fmt.Fprintf(stderr, "scan findings at or above %q present on pinned fragments (see 'gridctl ctx pins list')\n", failOn)
		return pinsExitDrift
	}
	return pinsExitOK
}

func fragmentFindingsAtOrAbove(pinned map[string]*ctxpins.FragmentPin, names []string, threshold string) bool {
	want := pins.SeverityRank(threshold)
	for _, name := range names {
		pin := pinned[name]
		if pin == nil {
			continue
		}
		for _, f := range pin.Findings {
			if pins.SeverityRank(f.Severity) >= want {
				return true
			}
		}
	}
	return false
}

type ctxPinsDiffDoc struct {
	SchemaVersion   int                 `json:"schema_version"`
	Fragment        string              `json:"fragment"`
	Status          string              `json:"status"`
	CompositeHash   string              `json:"composite_hash"`
	DocumentChanged bool                `json:"document_changed"`
	OldDocument     string              `json:"old_document,omitempty"`
	NewDocument     string              `json:"new_document,omitempty"`
	Findings        []pins.Finding      `json:"findings"`
	LastApproval    *pinapprovals.Entry `json:"last_approval,omitempty"`
}

func runCtxPinsDiff(ctx context.Context, stdout, stderr io.Writer, name, format, failOn string) int {
	store, src, err := loadCtxPins(ctx)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return pinsExitInfrastructure
	}
	vr, err := store.Verify(ctx, name, src)
	switch {
	case errors.Is(err, ctxpins.ErrDigestUnavailable):
		fmt.Fprintf(stderr, "fragment %q content could not be hashed; its pin fails closed as drift until the content is readable (%v)\n", name, err)
		return pinsExitInfrastructure
	case errors.Is(err, ctxpins.ErrNoDocument):
		fmt.Fprintf(stderr, "fragment %q not found in the context store (see 'gridctl ctx pins list')\n", name)
		return pinsExitInfrastructure
	case errors.Is(err, ctxpins.ErrNotPinned):
		fmt.Fprintf(stderr, "no pin found for fragment %q; it is pinned on the next 'gridctl ctx sync'\n", name)
		return pinsExitInfrastructure
	case err != nil:
		fmt.Fprintln(stderr, err)
		return pinsExitInfrastructure
	}
	doc := ctxPinsDiffDoc{
		SchemaVersion: ctxJSONSchemaVersion,
		Fragment:      name,
		Status:        vr.Status,
		CompositeHash: vr.CompositeHash,
		Findings:      []pins.Finding{},
	}
	if d := vr.Diff; d != nil {
		doc.DocumentChanged = d.DocumentChanged()
		doc.OldDocument = d.OldDocument
		doc.NewDocument = d.NewDocument
		doc.Findings = d.Findings
		if doc.Findings == nil {
			doc.Findings = []pins.Finding{}
		}
	}
	if pin, ok, err := store.Get(ctx, name); err == nil && ok && len(pin.Approvals) > 0 {
		latest := pin.Approvals[0]
		doc.LastApproval = &latest
	}
	if strings.EqualFold(format, "json") {
		if err := output.EncodeJSON(stdout, doc); err != nil {
			fmt.Fprintln(stderr, err)
			return pinsExitInfrastructure
		}
	} else {
		renderCtxPinsDiffText(stdout, doc)
	}
	if doc.Status == ctxpins.StatusDrift {
		return pinsExitDrift
	}
	pin, _, _ := store.Get(ctx, name)
	if failOn != "" && fragmentFindingsAtOrAbove(map[string]*ctxpins.FragmentPin{name: pin}, []string{name}, failOn) {
		fmt.Fprintf(stderr, "scan findings at or above %q present on this pinned fragment (see 'gridctl ctx pins list')\n", failOn)
		return pinsExitDrift
	}
	return pinsExitOK
}

func renderCtxPinsDiffText(w io.Writer, doc ctxPinsDiffDoc) {
	if doc.Status != ctxpins.StatusDrift {
		fmt.Fprintf(w, "  ✓ %s verified — content matches its pin\n", doc.Fragment)
		return
	}
	fmt.Fprintf(w, "  ✗ %s has pin drift\n", doc.Fragment)
	if doc.DocumentChanged {
		fmt.Fprintf(w, "    document changed (%d -> %d bytes); full before/after in '--format json' or the Pins workspace\n",
			len(doc.OldDocument), len(doc.NewDocument))
	} else {
		fmt.Fprintln(w, "    document unchanged")
	}
	for _, f := range doc.Findings {
		fmt.Fprintf(w, "    %s [%s/%s] %s: %s\n", findingGlyph(f.Severity), f.Code, f.Severity, f.Field, escapeNonPrintable(f.Message))
	}
	renderLastApproval(w, doc.LastApproval)
	line := fmt.Sprintf("    approve with: gridctl ctx pins approve %s --expect %s", doc.Fragment, doc.CompositeHash)
	if len(doc.Findings) > 0 {
		line += " --reason \"<why>\""
	}
	fmt.Fprintln(w, line)
}

func runCtxPinsApprove(ctx context.Context, stdout, stderr io.Writer, name, expectHash, reason string, evidence []string) error {
	cleaned, err := pinapprovals.ValidateReferences(evidence)
	if err != nil {
		return err
	}
	if err := pinapprovals.ValidateReason(reason); err != nil {
		return err
	}
	by, source := pinapprovals.LocalUser()
	if err := pinapprovals.ValidateApprover(by); err != nil {
		return err
	}
	store, src, err := loadCtxPins(ctx)
	if err != nil {
		fmt.Fprintln(stderr, err)
		os.Exit(pinsExitInfrastructure)
	}
	err = store.Approve(ctx, name, src, ctxpins.ApproveRequest{
		ExpectedHash:     expectHash,
		Reason:           reason,
		Evidence:         cleaned,
		ApprovedBy:       by,
		ApprovedBySource: source,
	})
	switch {
	case errors.Is(err, ctxpins.ErrDigestUnavailable):
		fmt.Fprintf(stderr, "fragment %q content could not be hashed; fix the unreadable file before approving (%v)\n", name, err)
		os.Exit(pinsExitInfrastructure)
	case errors.Is(err, ctxpins.ErrNoDocument):
		fmt.Fprintf(stderr, "fragment %q not found in the context store (for a deleted fragment, run 'gridctl ctx pins reset %s')\n", name, name)
		os.Exit(pinsExitInfrastructure)
	case errors.Is(err, ctxpins.ErrHashMismatch):
		return fmt.Errorf("fragment content changed since the reviewed diff (run 'gridctl ctx pins diff %s' to re-review)", name)
	case errors.Is(err, ctxpins.ErrReasonRequired):
		return fmt.Errorf("fragment %q carries unresolved advisory findings; pass --reason to approve over them", name)
	case err != nil:
		fmt.Fprintln(stderr, err)
		os.Exit(pinsExitInfrastructure)
	}
	fmt.Fprintf(stdout, "✓ Approved content update for fragment %s\n", name)
	printRecordedApproval(by, source, len(cleaned))
	return nil
}

func runCtxPinsReset(ctx context.Context, stdout, stderr io.Writer, name string) int {
	store, _, err := loadCtxPins(ctx)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return pinsExitInfrastructure
	}
	if err := store.Reset(ctx, name); err != nil {
		if errors.Is(err, ctxpins.ErrNotPinned) {
			fmt.Fprintf(stderr, "no pin found for fragment %q (see 'gridctl ctx pins list')\n", name)
			return pinsExitInfrastructure
		}
		fmt.Fprintln(stderr, err)
		return pinsExitInfrastructure
	}
	fmt.Fprintf(stdout, "✓ Pin reset for fragment %s. It is re-pinned on the next sync.\n", name)
	return pinsExitOK
}

func loadCtxPins(ctx context.Context) (*ctxpins.Store, ctxpins.Source, error) {
	mgr, err := newContextManager()
	if err != nil {
		return nil, nil, err
	}
	store := ctxpins.New()
	enabled, ignore := ctxpins.DeployedScanConfig()
	store.SetScanConfig(enabled, ignore)
	src, err := contextDocumentSource(ctx, mgr)
	if err != nil {
		return nil, nil, err
	}
	return store, src, nil
}
