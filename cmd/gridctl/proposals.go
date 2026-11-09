package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gridctl/gridctl/pkg/ctxpins"
	"github.com/gridctl/gridctl/pkg/hookpins"
	"github.com/gridctl/gridctl/pkg/output"
	"github.com/gridctl/gridctl/pkg/pinapprovals"
	"github.com/gridctl/gridctl/pkg/pins"
	"github.com/gridctl/gridctl/pkg/proposals"
	"github.com/gridctl/gridctl/pkg/skillpins"
	"github.com/gridctl/gridctl/pkg/skills"
	"github.com/gridctl/gridctl/pkg/state"

	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/spf13/cobra"
)

// Exit codes shared by `gridctl propose` and `gridctl proposals`.
const (
	proposalsExitOK             = 0
	proposalsExitRefused        = 1
	proposalsExitInfrastructure = 2
)

// proposalsJSONSchemaVersion versions the list and base documents. The
// stored record is versioned by its own file version.
const proposalsJSONSchemaVersion = 1

// proposalDocumentMaxBytes bounds a create document read from a file or
// stdin, matching the REST create body.
const proposalDocumentMaxBytes = 512 * 1024

var (
	proposeFormat string
	proposeJSON   *bool

	proposalsListStatus string
	proposalsListKind   string
	proposalsListFormat string
	proposalsListJSON   *bool
	proposalsListPlain  *bool

	proposalsShowStack  string
	proposalsShowFormat string
	proposalsShowJSON   *bool

	proposalsApproveStack    string
	proposalsApproveReason   string
	proposalsApproveEvidence []string
	proposalsApproveFormat   string
	proposalsApproveJSON     *bool

	proposalsRejectReason string
	proposalsRejectFormat string
	proposalsRejectJSON   *bool

	proposalsBaseStack  string
	proposalsBaseFormat string
	proposalsBaseJSON   *bool
)

var proposeCmd = &cobra.Command{
	Use:   "propose <file|->",
	Short: "Stage a change to a governed document for review",
	Long: `Stage a proposal: a unified diff against a skill, a context fragment (or
AGENTS.md when fragments mode is off), or a hook, held for human review.
Nothing is written to the target until 'gridctl proposals approve'.

The argument is a JSON create document, or '-' to read it from stdin:

  {"kind": "skill", "target": "incident-triage",
   "base_hash": "<from 'gridctl proposals base skill incident-triage'>",
   "diff": "--- a/SKILL.md\n+++ b/SKILL.md\n@@ ...",
   "classification": "knowledge", "prevalence": {"sessions": 14, "runs": 6},
   "evidence": ["session:abc123"], "reason": "...", "source": "dream"}

Kind agent is reserved and refused: agent pins do not exist yet.

Exit codes:
  0  proposal staged
  1  the document was refused (invalid field, unsupported kind or diff,
     credential-shaped value, missing target, or a diff that does not apply)
  2  infrastructure error (home unresolvable, unreadable file or store)`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		format, err := resolveFormat(proposeFormat, cmd.Flags().Changed("format"), *proposeJSON)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(proposalsExitInfrastructure)
		}
		exitProposals(runPropose(cmd.Context(), os.Stdin, os.Stdout, os.Stderr, args[0], format))
		return nil
	},
}

var proposalsCmd = &cobra.Command{
	Use:   "proposals",
	Short: "List, show, approve, and reject staged changes",
	Long: `Review proposals staged with 'gridctl propose'.

Approve is compare-and-swap on the proposal's base hash. Against a locally
authored skill or local fragment it applies the diff, re-pins the target,
and records the approval with the proposal's reason and evidence plus
proposal:<id>. Against an imported target (pack, git, or local import) or
any hook it records the verdict on the proposal only: nothing is written
and no pin changes. Reject records the verdict and keeps the proposal.`,
}

var proposalsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List proposals, pending first",
	Long: `List proposals, pending first and then newest first.

Files that cannot be read are reported as skipped with a category
(corrupt, newer_version, permission, or unreadable); their contents are
never printed.

Exit codes:
  0  listed
  2  infrastructure error`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		format, err := resolveFormat(proposalsListFormat, cmd.Flags().Changed("format"), *proposalsListJSON)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(proposalsExitInfrastructure)
		}
		if err := resolvePlain(*proposalsListPlain, format); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(proposalsExitInfrastructure)
		}
		exitProposals(runProposalsList(cmd.Context(), os.Stdout, os.Stderr, proposalsListStatus, proposalsListKind, format, *proposalsListPlain))
		return nil
	},
}

var proposalsShowCmd = &cobra.Command{
	Use:   "show <id>",
	Short: "Show one proposal with its diff, findings, and evidence",
	Long: `Show a proposal: target, path, provenance, base status (current or
stale), pin status, the stored diff, advisory findings, evidence, and the
verdict when present. --stack selects the skill pin store for pin status.

Exit codes:
  0  shown
  2  unknown id or infrastructure error`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		format, err := resolveFormat(proposalsShowFormat, cmd.Flags().Changed("format"), *proposalsShowJSON)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(proposalsExitInfrastructure)
		}
		exitProposals(runProposalsShow(cmd.Context(), os.Stdout, os.Stderr, args[0], proposalsShowStack, format))
		return nil
	},
}

var proposalsApproveCmd = &cobra.Command{
	Use:   "approve <id>",
	Short: "Approve a pending proposal",
	Long: `Approve a pending proposal.

The target must still hash to the proposal's base. On the apply path the
target must also show no pin drift. --reason replaces the proposal's
reason and is required when the proposal carries advisory findings and
has no reason of its own. Repeat --evidence to add opaque references.
Credential-shaped values are refused.

A skill approve routes through the running daemon for the resolved stack
so its pin store sees the re-pin.

Exit codes:
  0  approved
  1  refused (stale base, pin drift, reason required, not pending, apply
     failed, credential-shaped input) or applied with a re-pin error
  2  unknown id or infrastructure error`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		format, err := resolveFormat(proposalsApproveFormat, cmd.Flags().Changed("format"), *proposalsApproveJSON)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(proposalsExitInfrastructure)
		}
		exitProposals(runProposalsApprove(cmd.Context(), os.Stdout, os.Stderr, args[0], proposalsApproveStack, proposalsApproveReason, proposalsApproveEvidence, format))
		return nil
	},
}

var proposalsRejectCmd = &cobra.Command{
	Use:   "reject <id>",
	Short: "Reject a pending proposal",
	Long: `Reject a pending proposal. --reason is required. The verdict is recorded
on the proposal; nothing is written to the target or any approvals log.

Exit codes:
  0  rejected
  1  not pending, or the reason was refused
  2  unknown id or infrastructure error`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		format, err := resolveFormat(proposalsRejectFormat, cmd.Flags().Changed("format"), *proposalsRejectJSON)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(proposalsExitInfrastructure)
		}
		exitProposals(runProposalsReject(cmd.Context(), os.Stdout, os.Stderr, args[0], proposalsRejectReason, format))
		return nil
	},
}

var proposalsBaseCmd = &cobra.Command{
	Use:   "base <kind> <name>",
	Short: "Print a target's current composite hash for base_hash",
	Long: `Print the current composite hash of a skill, context fragment (or
AGENTS.md), or hook. Use it as base_hash in a create document. Read-only;
it needs no pin record, so it works for a target that was never pinned.
--stack applies to skills only.

Exit codes:
  0  printed
  1  unsupported kind or invalid name
  2  target not found or infrastructure error`,
	Args: cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		format, err := resolveFormat(proposalsBaseFormat, cmd.Flags().Changed("format"), *proposalsBaseJSON)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(proposalsExitInfrastructure)
		}
		exitProposals(runProposalsBase(cmd.Context(), os.Stdout, os.Stderr, args[0], args[1], proposalsBaseStack, format))
		return nil
	},
}

func init() {
	proposeCmd.Flags().StringVar(&proposeFormat, "format", "", "Output format: 'json' prints the stored record (default: text)")
	proposeJSON = addJSONAlias(proposeCmd)

	proposalsListCmd.Flags().StringVar(&proposalsListStatus, "status", "all", "Filter by status: pending, approved, rejected, or all")
	proposalsListCmd.Flags().StringVar(&proposalsListKind, "kind", "", "Filter by kind: skill, context, or hook")
	proposalsListCmd.Flags().StringVar(&proposalsListFormat, "format", "", "Output format: 'json' for machine-readable output (default: table)")
	proposalsListJSON = addJSONAlias(proposalsListCmd)
	proposalsListPlain = addPlainFlag(proposalsListCmd)

	proposalsShowCmd.Flags().StringVar(&proposalsShowStack, "stack", "", "Stack whose skill pin store reports pin status (auto-detected if only one stack is deployed)")
	proposalsShowCmd.Flags().StringVar(&proposalsShowFormat, "format", "", "Output format: 'json' for machine-readable output (default: text)")
	proposalsShowJSON = addJSONAlias(proposalsShowCmd)

	proposalsApproveCmd.Flags().StringVar(&proposalsApproveStack, "stack", "", "Stack whose skill pin store is re-pinned (auto-detected if only one stack is deployed)")
	proposalsApproveCmd.Flags().StringVar(&proposalsApproveReason, "reason", "", "Reason for this approval; replaces the proposal's reason (required when findings exist and the proposal has none)")
	proposalsApproveCmd.Flags().StringArrayVar(&proposalsApproveEvidence, "evidence", nil, "Opaque evidence reference (repeatable). Stored as text and never fetched")
	proposalsApproveCmd.Flags().StringVar(&proposalsApproveFormat, "format", "", "Output format: 'json' prints the updated record (default: text)")
	proposalsApproveJSON = addJSONAlias(proposalsApproveCmd)

	proposalsRejectCmd.Flags().StringVar(&proposalsRejectReason, "reason", "", "Why the proposal is rejected (required)")
	proposalsRejectCmd.Flags().StringVar(&proposalsRejectFormat, "format", "", "Output format: 'json' prints the updated record (default: text)")
	proposalsRejectJSON = addJSONAlias(proposalsRejectCmd)

	proposalsBaseCmd.Flags().StringVar(&proposalsBaseStack, "stack", "", "Stack name (skills only)")
	proposalsBaseCmd.Flags().StringVar(&proposalsBaseFormat, "format", "", "Output format: 'json' for machine-readable output (default: the hash alone)")
	proposalsBaseJSON = addJSONAlias(proposalsBaseCmd)

	proposalsCmd.AddCommand(proposalsListCmd)
	proposalsCmd.AddCommand(proposalsShowCmd)
	proposalsCmd.AddCommand(proposalsApproveCmd)
	proposalsCmd.AddCommand(proposalsRejectCmd)
	proposalsCmd.AddCommand(proposalsBaseCmd)
}

func exitProposals(code int) {
	if code != proposalsExitOK {
		os.Exit(code)
	}
}

// proposalInputRefusal reports an approvals input refusal: a credential
// shape, a bad or excess reference, an overlong reason, or a bad approver.
func proposalInputRefusal(err error) bool {
	return errors.Is(err, pinapprovals.ErrCredentialShaped) ||
		errors.Is(err, pinapprovals.ErrInvalidReference) ||
		errors.Is(err, pinapprovals.ErrTooManyReferences) ||
		errors.Is(err, pinapprovals.ErrReasonTooLong) ||
		errors.Is(err, pinapprovals.ErrInvalidApprover)
}

// proposalExit maps a store error to an exit code. targetNotFound is the
// code for ErrTargetNotFound, which create treats as a refusal and base
// treats as an infrastructure error.
func proposalExit(err error, targetNotFound int) int {
	switch {
	case err == nil:
		return proposalsExitOK
	case errors.Is(err, proposals.ErrPinFailed):
		return proposalsExitRefused
	case errors.Is(err, proposals.ErrStoreUnavailable),
		errors.Is(err, proposals.ErrNotFound),
		errors.Is(err, proposals.ErrCorrupt),
		errors.Is(err, proposals.ErrNewerVersion):
		return proposalsExitInfrastructure
	case errors.Is(err, proposals.ErrTargetNotFound):
		return targetNotFound
	case errors.Is(err, proposals.ErrInvalidInput),
		errors.Is(err, proposals.ErrKindUnsupported),
		errors.Is(err, proposals.ErrDiffUnsupported),
		errors.Is(err, proposals.ErrBaseStale),
		errors.Is(err, proposals.ErrTargetDrift),
		errors.Is(err, proposals.ErrReasonRequired),
		errors.Is(err, proposals.ErrNotPending),
		errors.Is(err, proposals.ErrApplyFailed),
		proposalInputRefusal(err):
		return proposalsExitRefused
	default:
		return proposalsExitInfrastructure
	}
}

// printProposalError prints a store error without the package prefix.
func printProposalError(w io.Writer, err error) {
	fmt.Fprintln(w, sanitizeTerminal(strings.TrimPrefix(err.Error(), "proposals: ")))
}

// proposalTargets builds a resolver from the on-disk stores a kind needs.
// withSkillPins loads the resolved stack's skill pin store; a stack that
// cannot be resolved leaves it nil and returns the reason as stackProblem,
// so read paths report pin status unknown and an apply-path approve reports
// the store as unavailable. An unknown kind gets an empty resolver; the
// store refuses the kind before resolving.
func proposalTargets(kind, stackFlag string, withSkillPins bool) (*proposals.Targets, string, error) {
	targets := &proposals.Targets{LockPath: skills.LockFilePath()}
	switch kind {
	case proposals.KindSkill:
		reg, err := loadRegistry()
		if err != nil {
			return nil, "", err
		}
		targets.Registry = reg
		if !withSkillPins {
			return targets, "", nil
		}
		stack, serr := resolveStackNamed(stackFlag)
		if serr != nil {
			return targets, serr.Error(), nil
		}
		ps := skillpins.New(stack)
		if err := ps.Load(); err != nil {
			return nil, "", err
		}
		targets.SkillPins = ps
		targets.Stack = stack
	case proposals.KindContext:
		mgr, err := newContextManager()
		if err != nil {
			return nil, "", err
		}
		store := ctxpins.New()
		enabled, ignore := ctxpins.DeployedScanConfig()
		store.SetScanConfig(enabled, ignore)
		targets.Context = mgr
		targets.ContextPins = store
	case proposals.KindHook:
		reg, err := loadRegistry()
		if err != nil {
			return nil, "", err
		}
		hp := hookpins.New()
		if err := hp.Load(); err != nil {
			return nil, "", err
		}
		targets.Registry = reg
		targets.HookPins = hp
	}
	return targets, "", nil
}

func readProposalDocument(stdin io.Reader, src string) ([]byte, int, error) {
	var r io.Reader
	if src == "-" {
		r = stdin
	} else {
		f, err := os.Open(src) // #nosec G304 -- the operator names the document to read
		if err != nil {
			return nil, proposalsExitInfrastructure, fmt.Errorf("reading proposal document: %w", err)
		}
		defer f.Close()
		r = f
	}
	data, err := io.ReadAll(io.LimitReader(r, proposalDocumentMaxBytes+1))
	if err != nil {
		return nil, proposalsExitInfrastructure, fmt.Errorf("reading proposal document: %w", err)
	}
	if len(data) > proposalDocumentMaxBytes {
		return nil, proposalsExitRefused, fmt.Errorf("proposal document is larger than %d bytes", proposalDocumentMaxBytes)
	}
	return data, proposalsExitOK, nil
}

func runPropose(ctx context.Context, stdin io.Reader, stdout, stderr io.Writer, src, format string) int {
	data, code, err := readProposalDocument(stdin, src)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return code
	}
	var req proposals.CreateRequest
	if err := json.Unmarshal(data, &req); err != nil {
		fmt.Fprintf(stderr, "invalid proposal document: %v\n", err)
		return proposalsExitRefused
	}
	targets, _, err := proposalTargets(req.Kind, "", false)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return proposalsExitInfrastructure
	}
	p, err := proposals.New().Create(ctx, req, targets)
	if err != nil {
		printProposalError(stderr, err)
		return proposalExit(err, proposalsExitRefused)
	}
	if strings.EqualFold(format, "json") {
		if err := output.EncodeJSON(stdout, p); err != nil {
			fmt.Fprintln(stderr, err)
			return proposalsExitInfrastructure
		}
		return proposalsExitOK
	}
	renderProposalCreated(stdout, p)
	return proposalsExitOK
}

// proposalPathLabel is "apply (local)", "record (pack NAME)", "record
// (imported from DIR)", or "record (git)".
func proposalPathLabel(p *proposals.Proposal) string {
	if p.Path == proposals.PathApply {
		return "apply (local)"
	}
	switch p.Provenance.Kind {
	case proposals.ProvenancePack:
		if p.Provenance.Pack != "" {
			return "record (pack " + escapeNonPrintable(p.Provenance.Pack) + ")"
		}
		return "record (pack)"
	case proposals.ProvenanceLocalImport:
		if p.Provenance.SourceDir != "" {
			return "record (imported from " + escapeNonPrintable(p.Provenance.SourceDir) + ")"
		}
		return "record (local import)"
	case "":
		return "record"
	default:
		return "record (" + escapeNonPrintable(p.Provenance.Kind) + ")"
	}
}

// proposalArrival says how a record-path change reaches the target. Hooks
// import only through packs.
func proposalArrival(p *proposals.Proposal) string {
	if p.Kind == proposals.KindHook || p.Provenance.Kind == proposals.ProvenancePack {
		return "the change arrives through 'gridctl pack add'"
	}
	if p.Provenance.Kind == proposals.ProvenanceLocalImport {
		if p.Provenance.SourceDir != "" {
			return "edit " + escapeNonPrintable(p.Provenance.SourceDir) + " and run 'gridctl skill update'"
		}
		return "edit the source directory and run 'gridctl skill update'"
	}
	return "the change arrives through 'gridctl skill update'"
}

func proposalFindingsLabel(n int) string {
	if n == 0 {
		return "none"
	}
	return fmt.Sprintf("%d advisory", n)
}

func renderProposalCreated(w io.Writer, p *proposals.Proposal) {
	fmt.Fprintf(w, "✓ Proposal %s staged\n", p.ID)
	fmt.Fprintf(w, "  target:    %s/%s\n", p.Kind, escapeNonPrintable(p.Target))
	fmt.Fprintf(w, "  path:      %s\n", proposalPathLabel(p))
	if p.Path == proposals.PathRecord {
		fmt.Fprintf(w, "             approve records the verdict on the proposal; %s\n", proposalArrival(p))
	}
	fmt.Fprintf(w, "  findings:  %s\n", proposalFindingsLabel(len(p.Findings)))
	if p.BaseCurrent {
		fmt.Fprintln(w, "  base:      current")
	} else {
		fmt.Fprintln(w, "  base:      stale (target changed since the proposer read it; approve will refuse until re-based)")
	}
	fmt.Fprintf(w, "  review:    gridctl proposals show %s\n", p.ID)
}

func validProposalStatusFilter(v string) bool {
	switch v {
	case "", "all", proposals.StatusPending, proposals.StatusApproved, proposals.StatusRejected:
		return true
	}
	return false
}

func validProposalKindFilter(v string) bool {
	switch v {
	case "", proposals.KindSkill, proposals.KindContext, proposals.KindHook:
		return true
	}
	return false
}

// proposalsListDoc is the list document shared with GET /api/proposals.
type proposalsListDoc struct {
	SchemaVersion int                   `json:"schema_version"`
	Proposals     []*proposals.Proposal `json:"proposals"`
	Skipped       []proposals.Skipped   `json:"skipped"`
	Truncated     bool                  `json:"truncated"`
}

func runProposalsList(ctx context.Context, stdout, stderr io.Writer, status, kind, format string, plain bool) int {
	if !validProposalStatusFilter(status) {
		fmt.Fprintf(stderr, "invalid --status %q: want pending, approved, rejected, or all\n", status)
		return proposalsExitInfrastructure
	}
	if !validProposalKindFilter(kind) {
		fmt.Fprintf(stderr, "invalid --kind %q: want skill, context, or hook\n", kind)
		return proposalsExitInfrastructure
	}
	res, err := proposals.New().List(ctx, proposals.Filter{Status: status, Kind: kind})
	if err != nil {
		printProposalError(stderr, err)
		return proposalsExitInfrastructure
	}
	doc := proposalsListDoc{
		SchemaVersion: proposalsJSONSchemaVersion,
		Proposals:     res.Proposals,
		Skipped:       res.Skipped,
		Truncated:     res.Truncated,
	}
	if doc.Proposals == nil {
		doc.Proposals = []*proposals.Proposal{}
	}
	if doc.Skipped == nil {
		doc.Skipped = []proposals.Skipped{}
	}
	if strings.EqualFold(format, "json") {
		if err := output.EncodeJSON(stdout, doc); err != nil {
			fmt.Fprintln(stderr, err)
			return proposalsExitInfrastructure
		}
		return proposalsExitOK
	}
	for _, s := range doc.Skipped {
		fmt.Fprintf(stderr, "warning: skipped %s (%s)\n", escapeNonPrintable(s.Path), s.Category)
	}
	if doc.Truncated {
		fmt.Fprintf(stderr, "warning: listed the first %d proposal files only\n", proposals.MaxListFiles)
	}
	if len(doc.Proposals) == 0 {
		fmt.Fprintln(stdout, "No proposals found. Stage one with 'gridctl propose'.")
		return proposalsExitOK
	}
	t := output.NewTableWriter(stdout, plain)
	t.AppendHeader(table.Row{"ID", "KIND", "TARGET", "CLASS", "PATH", "STATUS", "FINDINGS", "CREATED"})
	for _, p := range doc.Proposals {
		t.AppendRow(table.Row{
			p.ID,
			p.Kind,
			escapeNonPrintable(p.Target),
			p.Classification,
			p.Path,
			p.Status,
			len(p.Findings),
			p.CreatedAt.UTC().Format("2006-01-02 15:04"),
		})
	}
	t.Render()
	return proposalsExitOK
}

func printProposalNotFound(w io.Writer, id string) {
	fmt.Fprintf(w, "proposal %q not found (see 'gridctl proposals list')\n", escapeNonPrintable(id))
}

func runProposalsShow(ctx context.Context, stdout, stderr io.Writer, id, stack, format string) int {
	p, err := proposals.New().Get(ctx, id)
	if err != nil {
		if errors.Is(err, proposals.ErrNotFound) {
			printProposalNotFound(stderr, id)
		} else {
			printProposalError(stderr, err)
		}
		return proposalsExitInfrastructure
	}
	targets, _, terr := proposalTargets(p.Kind, stack, true)
	if terr != nil {
		proposals.Annotate(ctx, p, nil)
	} else {
		proposals.Annotate(ctx, p, targets)
	}
	if strings.EqualFold(format, "json") {
		if err := output.EncodeJSON(stdout, p); err != nil {
			fmt.Fprintln(stderr, err)
			return proposalsExitInfrastructure
		}
		return proposalsExitOK
	}
	renderProposalText(stdout, p)
	return proposalsExitOK
}

func renderProposalText(w io.Writer, p *proposals.Proposal) {
	fmt.Fprintf(w, "Proposal %s\n", p.ID)
	fmt.Fprintf(w, "  target:          %s/%s\n", p.Kind, escapeNonPrintable(p.Target))
	fmt.Fprintf(w, "  status:          %s\n", p.Status)
	fmt.Fprintf(w, "  classification:  %s\n", p.Classification)
	fmt.Fprintf(w, "  path:            %s\n", proposalPathLabel(p))
	if p.Path == proposals.PathRecord {
		fmt.Fprintf(w, "                   approve records the verdict on the proposal; %s\n", proposalArrival(p))
	}
	fmt.Fprintf(w, "  provenance:      %s\n", escapeNonPrintable(p.Provenance.Kind))
	if p.BaseCurrent {
		fmt.Fprintf(w, "  base:            current (%s)\n", shortPinHash(p.BaseHash))
	} else {
		fmt.Fprintf(w, "  base:            stale (%s; the target changed since the proposer read it, so approve will refuse)\n", shortPinHash(p.BaseHash))
	}
	fmt.Fprintf(w, "  pin:             %s\n", p.PinStatus)
	if len(p.Files) > 0 {
		files := make([]string, len(p.Files))
		for i, f := range p.Files {
			files[i] = escapeNonPrintable(f)
		}
		fmt.Fprintf(w, "  files:           %s\n", strings.Join(files, ", "))
	}
	created := p.CreatedAt.UTC().Format(time.RFC3339)
	if p.CreatedBy != "" {
		created += fmt.Sprintf(" by %s (%s)", escapeNonPrintable(p.CreatedBy), escapeNonPrintable(p.CreatedBySource))
	}
	fmt.Fprintf(w, "  created:         %s\n", created)

	fmt.Fprintln(w, "\nDiff")
	for _, line := range strings.Split(strings.TrimRight(strings.ReplaceAll(p.Diff, "\r\n", "\n"), "\n"), "\n") {
		fmt.Fprintln(w, "  "+escapeNonPrintable(line))
	}

	fmt.Fprintln(w, "\nFindings (advisory)")
	renderProposalFindings(w, p.Findings)

	fmt.Fprintln(w, "\nEvidence")
	if len(p.Evidence) == 0 {
		fmt.Fprintln(w, "  none")
	}
	for i, ref := range p.Evidence {
		fmt.Fprintf(w, "  %d. %s\n", i+1, escapeNonPrintable(ref))
	}

	fmt.Fprintf(w, "\nPrevalence: %d sessions, %d runs\n", p.Prevalence.Sessions, p.Prevalence.Runs)
	fmt.Fprintln(w, "\nReason")
	fmt.Fprintln(w, "  "+orNone(escapeNonPrintable(p.Reason)))
	fmt.Fprintln(w, "\nSource")
	fmt.Fprintln(w, "  "+orNone(escapeNonPrintable(p.Source)))

	if v := p.Verdict; v != nil {
		fmt.Fprintln(w, "\nVerdict")
		line := fmt.Sprintf("  %s %s", p.Status, v.At.UTC().Format(time.RFC3339))
		if v.By != "" {
			line += fmt.Sprintf(" by %s (%s)", escapeNonPrintable(v.By), escapeNonPrintable(v.BySource))
		}
		fmt.Fprintln(w, line)
		if p.Status == proposals.StatusApproved {
			if v.Applied {
				applied := "  applied; new hash " + shortPinHash(v.NewHash)
				if v.Stack != "" {
					applied += " (stack " + escapeNonPrintable(v.Stack) + ")"
				}
				fmt.Fprintln(w, applied)
			} else {
				fmt.Fprintln(w, "  verdict only; nothing was written and no pin changed")
			}
		}
		if v.Reason != "" {
			fmt.Fprintln(w, "  reason: "+escapeNonPrintable(v.Reason))
		}
		for _, ref := range v.Evidence {
			fmt.Fprintln(w, "  evidence: "+escapeNonPrintable(ref))
		}
		if v.PinError != "" {
			fmt.Fprintln(w, "  pin error: "+escapeNonPrintable(v.PinError))
		}
	}
}

// renderProposalFindings uses the same line shape as the pins diff views.
func renderProposalFindings(w io.Writer, findings []pins.Finding) {
	if len(findings) == 0 {
		fmt.Fprintln(w, "  none")
		return
	}
	for _, f := range findings {
		fmt.Fprintf(w, "  %s [%s/%s] %s: %s\n", findingGlyph(f.Severity), f.Code, f.Severity, escapeNonPrintable(f.Field), escapeNonPrintable(f.Message))
	}
}

func orNone(s string) string {
	if s == "" {
		return "none"
	}
	return s
}

// runningProposalDaemon returns the running daemon for the resolved stack,
// or nil, mirroring runningSkillPinsDaemon with the proposals --stack flag.
func runningProposalDaemon(stackFlag string) *state.DaemonState {
	stack, err := resolveStackNamed(stackFlag)
	if err != nil {
		return nil
	}
	st, err := state.Load(stack)
	if err != nil || !state.IsRunning(st) {
		return nil
	}
	return st
}

func runProposalsApprove(ctx context.Context, stdout, stderr io.Writer, id, stack, reason string, evidence []string, format string) int {
	cleaned, err := pinapprovals.ValidateReferences(evidence)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return proposalsExitRefused
	}
	if err := pinapprovals.ValidateReason(reason); err != nil {
		fmt.Fprintln(stderr, err)
		return proposalsExitRefused
	}
	by, source := pinapprovals.LocalUser()
	if err := pinapprovals.ValidateApprover(by); err != nil {
		fmt.Fprintln(stderr, err)
		return proposalsExitRefused
	}
	store := proposals.New()
	current, err := store.Get(ctx, id)
	if err != nil {
		if errors.Is(err, proposals.ErrNotFound) {
			printProposalNotFound(stderr, id)
		} else {
			printProposalError(stderr, err)
		}
		return proposalsExitInfrastructure
	}
	if current.Kind == proposals.KindSkill {
		if st := runningProposalDaemon(stack); st != nil {
			return approveProposalViaAPI(stdout, stderr, st, id, reason, cleaned, by, format)
		}
	}
	targets, stackProblem, err := proposalTargets(current.Kind, stack, true)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return proposalsExitInfrastructure
	}
	p, err := store.Approve(ctx, id, proposals.ApproveRequest{
		Reason:           reason,
		Evidence:         cleaned,
		ApprovedBy:       by,
		ApprovedBySource: source,
	}, targets)
	if err != nil && p == nil {
		printProposalError(stderr, err)
		if errors.Is(err, proposals.ErrStoreUnavailable) && stackProblem != "" {
			fmt.Fprintf(stderr, "skill pin store unavailable: %s\n", stackProblem)
		}
		return proposalExit(err, proposalsExitRefused)
	}
	return reportProposalApproval(stdout, stderr, p, err, format)
}

// reportProposalApproval prints an approved record. pinErr is the re-pin
// failure after a successful write; the record is approved either way.
func reportProposalApproval(stdout, stderr io.Writer, p *proposals.Proposal, pinErr error, format string) int {
	if strings.EqualFold(format, "json") {
		if err := output.EncodeJSON(stdout, p); err != nil {
			fmt.Fprintln(stderr, err)
			return proposalsExitInfrastructure
		}
	} else if pinErr == nil {
		renderProposalApproved(stdout, p)
	}
	if pinErr != nil {
		printProposalError(stderr, pinErr)
		return proposalsExitRefused
	}
	return proposalsExitOK
}

func renderProposalApproved(w io.Writer, p *proposals.Proposal) {
	v := p.Verdict
	if v == nil {
		fmt.Fprintf(w, "✓ Approved proposal %s\n", p.ID)
		return
	}
	target := p.Kind + " " + escapeNonPrintable(p.Target)
	if v.Applied {
		fmt.Fprintf(w, "✓ Approved proposal %s: applied to %s and re-pinned\n", p.ID, target)
		fprintRecordedApproval(w, escapeNonPrintable(v.By), escapeNonPrintable(v.BySource), len(v.Evidence))
		return
	}
	label := strings.TrimPrefix(proposalPathLabel(p), "record ")
	arrival := proposalArrival(p)
	fmt.Fprintf(w, "✓ Approved proposal %s: verdict recorded for %s %s; nothing was written and no pin changed. %s%s; cite proposal:%s when approving that drift.\n",
		p.ID, target, label, strings.ToUpper(arrival[:1]), arrival[1:], p.ID)
}

// approveProposalViaAPI routes a skill approve through the running daemon
// so its in-memory skill pin store sees the re-pin. The id was already
// found locally, so a 404 here is a missing target.
func approveProposalViaAPI(stdout, stderr io.Writer, st *state.DaemonState, id, reason string, evidence []string, approvedBy, format string) int {
	payload, err := json.Marshal(map[string]any{
		"reason":      reason,
		"evidence":    evidence,
		"approved_by": approvedBy,
	})
	if err != nil {
		fmt.Fprintf(stderr, "encoding request: %v\n", err)
		return proposalsExitInfrastructure
	}
	api := newDaemonAPIFor(*st, pinsAPITimeout)
	req, err := http.NewRequest(http.MethodPost, api.URL("/api/proposals/"+id+"/approve"), strings.NewReader(string(payload)))
	if err != nil {
		fmt.Fprintf(stderr, "creating request: %v\n", err)
		return proposalsExitInfrastructure
	}
	req.Header.Set("Content-Type", "application/json")
	api.authorize(req)
	resp, err := api.client.Do(req)
	if err != nil {
		fmt.Fprintf(stderr, "calling proposals API: %v\n", err)
		return proposalsExitInfrastructure
	}
	defer resp.Body.Close()
	if err := api.describeUnauthorized(resp); err != nil {
		fmt.Fprintln(stderr, err)
		return proposalsExitInfrastructure
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, proposalDocumentMaxBytes*2))
	if err != nil {
		fmt.Fprintf(stderr, "reading response: %v\n", err)
		return proposalsExitInfrastructure
	}
	switch resp.StatusCode {
	case http.StatusOK:
		var p proposals.Proposal
		if err := json.Unmarshal(body, &p); err != nil {
			fmt.Fprintf(stderr, "decoding response: %v\n", err)
			return proposalsExitInfrastructure
		}
		return reportProposalApproval(stdout, stderr, &p, nil, format)
	case http.StatusInternalServerError:
		var p proposals.Proposal
		if err := json.Unmarshal(body, &p); err == nil && p.Status == proposals.StatusApproved && p.Verdict != nil && p.Verdict.PinError != "" {
			return reportProposalApproval(stdout, stderr, &p, errors.New(apiErrorMessage(body, "re-pinning failed")), format)
		}
		fmt.Fprintln(stderr, sanitizeTerminal(apiErrorMessage(body, "approve failed")))
		return proposalsExitInfrastructure
	case http.StatusBadRequest, http.StatusNotFound, http.StatusConflict, http.StatusUnprocessableEntity:
		fmt.Fprintln(stderr, sanitizeTerminal(apiErrorMessage(body, "approve refused")))
		return proposalsExitRefused
	default:
		fmt.Fprintln(stderr, sanitizeTerminal(apiErrorMessage(body, "approve failed")))
		return proposalsExitInfrastructure
	}
}

func runProposalsReject(ctx context.Context, stdout, stderr io.Writer, id, reason, format string) int {
	by, source := pinapprovals.LocalUser()
	p, err := proposals.New().Reject(ctx, id, proposals.RejectRequest{
		Reason:           reason,
		ApprovedBy:       by,
		ApprovedBySource: source,
	})
	if err != nil {
		if errors.Is(err, proposals.ErrNotFound) {
			printProposalNotFound(stderr, id)
		} else {
			printProposalError(stderr, err)
		}
		return proposalExit(err, proposalsExitRefused)
	}
	if strings.EqualFold(format, "json") {
		if err := output.EncodeJSON(stdout, p); err != nil {
			fmt.Fprintln(stderr, err)
			return proposalsExitInfrastructure
		}
		return proposalsExitOK
	}
	fmt.Fprintf(stdout, "✓ Rejected proposal %s (reason recorded). Nothing was written.\n", p.ID)
	return proposalsExitOK
}

// proposalsBaseDoc is the machine-readable document for `proposals base`.
type proposalsBaseDoc struct {
	SchemaVersion int    `json:"schema_version"`
	Kind          string `json:"kind"`
	Target        string `json:"target"`
	CompositeHash string `json:"composite_hash"`
}

func runProposalsBase(ctx context.Context, stdout, stderr io.Writer, kind, name, stack, format string) int {
	if stack != "" && kind != proposals.KindSkill {
		fmt.Fprintln(stderr, "--stack applies to skill targets only; context and hook targets are stackless")
		return proposalsExitInfrastructure
	}
	targets, _, err := proposalTargets(kind, stack, false)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return proposalsExitInfrastructure
	}
	target, err := targets.Resolve(ctx, kind, name)
	if err == nil {
		var hash string
		hash, err = target.CurrentHash(ctx)
		if err == nil {
			if strings.EqualFold(format, "json") {
				if err := output.EncodeJSON(stdout, proposalsBaseDoc{
					SchemaVersion: proposalsJSONSchemaVersion,
					Kind:          kind,
					Target:        name,
					CompositeHash: hash,
				}); err != nil {
					fmt.Fprintln(stderr, err)
					return proposalsExitInfrastructure
				}
				return proposalsExitOK
			}
			fmt.Fprintln(stdout, hash)
			return proposalsExitOK
		}
	}
	printProposalError(stderr, err)
	switch {
	case errors.Is(err, proposals.ErrKindUnsupported), errors.Is(err, proposals.ErrInvalidInput):
		return proposalsExitRefused
	default:
		return proposalsExitInfrastructure
	}
}
