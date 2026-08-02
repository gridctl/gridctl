package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/gridctl/gridctl/pkg/output"
	"github.com/gridctl/gridctl/pkg/pins"
	"github.com/gridctl/gridctl/pkg/registry"
	"github.com/gridctl/gridctl/pkg/skillpins"
	"github.com/gridctl/gridctl/pkg/state"

	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/spf13/cobra"
)

// skillPinsJSONSchemaVersion versions the JSON documents this command family
// emits. Bump on breaking shape changes; additions are allowed in place.
const skillPinsJSONSchemaVersion = 1

var (
	skillPinsStack         string
	skillPinsListFormat    string
	skillPinsListJSON      *bool
	skillPinsListPlain     *bool
	skillPinsVerifyFormat  string
	skillPinsVerifyJSON    *bool
	skillPinsVerifyFailOn  string
	skillPinsDiffFormat    string
	skillPinsDiffJSON      *bool
	skillPinsDiffFailOn    string
	skillPinsApproveExpect string
	skillPinsApproveReason string
)

var skillPinsCmd = &cobra.Command{
	Use:   "pins",
	Short: "Manage content pins for registry skills",
	Long: `Inspect, verify, approve, and reset TOFU content pins for skill documents.

Skill pins record per-file digests over a skill's whole document set (the
canonical SKILL.md plus supporting files). Content changes surface as "pin
drift" that persists until approved or reset — distinct from the Library's
sync drift, which compares local edits against the last git import.

Not to be confused with 'gridctl skill pin <name> <ref>' (singular), which
pins a git skill SOURCE to a ref. Servers are governed by 'gridctl pins'.`,
}

var skillPinsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List pin status for all skills in a stack",
	Long: `List pin status for every pinned skill.

Default output is a styled table; use '--format json' for machine-readable
output including per-file digests, provenance, and advisory findings.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		format, err := resolveFormat(skillPinsListFormat, cmd.Flags().Changed("format"), *skillPinsListJSON)
		if err != nil {
			return err
		}
		if err := resolvePlain(*skillPinsListPlain, format); err != nil {
			return err
		}
		return runSkillPinsList(format)
	},
}

var skillPinsVerifyCmd = &cobra.Command{
	Use:   "verify [skill]",
	Short: "Verify skill content against pins",
	Long: `Recompute every pinned skill's digests against the registry on disk and
report drift.

Exit codes:
  0  all pins verified (or nothing pinned yet)
  1  pin drift detected, or scan findings at or above the
     --fail-on-findings severity ('warn' or 'critical') on pinned skills
  2  infrastructure error (no stack, unreadable pin store, unknown skill)`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		format, err := resolveFormat(skillPinsVerifyFormat, cmd.Flags().Changed("format"), *skillPinsVerifyJSON)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(pinsExitInfrastructure)
		}
		skill := ""
		if len(args) == 1 {
			skill = args[0]
		}
		if err := validateFailOnFindings(skillPinsVerifyFailOn); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(pinsExitInfrastructure)
		}
		exit := runSkillPinsVerify(os.Stdout, os.Stderr, skill, format, skillPinsVerifyFailOn)
		if exit != pinsExitOK {
			os.Exit(exit)
		}
		return nil
	},
}

var skillPinsDiffCmd = &cobra.Command{
	Use:   "diff <skill>",
	Short: "Show what changed for a drifted skill",
	Long: `Show how a skill's document set moved since its pin: whether the
canonical SKILL.md changed, which supporting files were added, removed, or
modified, and any advisory findings on the new content.

The JSON output includes the pinned and current canonical documents plus the
composite_hash for 'skill pins approve --expect'. Findings are advisory and
never affect the exit code unless --fail-on-findings is passed.

Exit codes:
  0  no pin drift
  1  pin drift detected, or scan findings at or above --fail-on-findings
  2  infrastructure error (no stack, skill not found or not pinned)`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		format, err := resolveFormat(skillPinsDiffFormat, cmd.Flags().Changed("format"), *skillPinsDiffJSON)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(pinsExitInfrastructure)
		}
		if err := validateFailOnFindings(skillPinsDiffFailOn); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(pinsExitInfrastructure)
		}
		exit := runSkillPinsDiff(os.Stdout, os.Stderr, args[0], format, skillPinsDiffFailOn)
		if exit != pinsExitOK {
			os.Exit(exit)
		}
		return nil
	},
}

var skillPinsApproveCmd = &cobra.Command{
	Use:   "approve <skill>",
	Short: "Approve content changes for a skill",
	Long: `Re-pin a skill's current document set, clearing pin drift.

Pass --expect with the composite_hash from 'skill pins diff --format json'
to bind the approval to the reviewed content: if the skill changes again
between review and approval, the approve is rejected.

A skill whose current content carries unresolved advisory findings requires
--reason; the justification is persisted on the pin record.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runSkillPinsApprove(args[0], skillPinsApproveExpect, skillPinsApproveReason)
	},
}

var skillPinsResetCmd = &cobra.Command{
	Use:   "reset <skill>",
	Short: "Delete the pin for a skill",
	Long:  "Remove a skill's pin record. It is re-pinned fresh on the next registry refresh.",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runSkillPinsReset(args[0])
	},
}

func init() {
	skillPinsCmd.PersistentFlags().StringVar(&skillPinsStack, "stack", "", "Stack name (auto-detected if only one stack is deployed)")

	skillPinsListCmd.Flags().StringVar(&skillPinsListFormat, "format", "", "Output format: 'json' for machine-readable output (default: table)")
	skillPinsListJSON = addJSONAlias(skillPinsListCmd)
	skillPinsListPlain = addPlainFlag(skillPinsListCmd)

	skillPinsVerifyCmd.Flags().StringVar(&skillPinsVerifyFormat, "format", "", "Output format: 'json' for machine-readable output (default: text)")
	skillPinsVerifyJSON = addJSONAlias(skillPinsVerifyCmd)
	skillPinsVerifyCmd.Flags().StringVar(&skillPinsVerifyFailOn, "fail-on-findings", "",
		"Exit 1 when scan findings at or above this severity exist: 'warn' or 'critical' (default: findings never affect the exit code)")

	skillPinsDiffCmd.Flags().StringVar(&skillPinsDiffFormat, "format", "", "Output format: 'json' for machine-readable output (default: text)")
	skillPinsDiffJSON = addJSONAlias(skillPinsDiffCmd)
	skillPinsDiffCmd.Flags().StringVar(&skillPinsDiffFailOn, "fail-on-findings", "",
		"Exit 1 when scan findings at or above this severity exist: 'warn' or 'critical' (default: findings never affect the exit code)")

	skillPinsApproveCmd.Flags().StringVar(&skillPinsApproveExpect, "expect", "", "Reviewed composite_hash from 'skill pins diff'; approval is rejected if the content no longer matches")
	skillPinsApproveCmd.Flags().StringVar(&skillPinsApproveReason, "reason", "", "Justification for approving over unresolved advisory findings")

	skillPinsCmd.AddCommand(skillPinsListCmd)
	skillPinsCmd.AddCommand(skillPinsVerifyCmd)
	skillPinsCmd.AddCommand(skillPinsDiffCmd)
	skillPinsCmd.AddCommand(skillPinsApproveCmd)
	skillPinsCmd.AddCommand(skillPinsResetCmd)
	skillCmd.AddCommand(skillPinsCmd)
}

// validateFailOnFindings rejects unknown --fail-on-findings values up front.
func validateFailOnFindings(v string) error {
	if v != "" && v != pins.SeverityWarn && v != pins.SeverityCritical {
		return fmt.Errorf("invalid --fail-on-findings value %q: want 'warn' or 'critical'", v)
	}
	return nil
}

// loadSkillPinsForCLI resolves the stack and reads its skill pin store and
// the registry from disk. Both live on disk, so unlike tool pins no daemon
// is required for read paths.
func loadSkillPinsForCLI() (string, *skillpins.Store, *registry.Store, error) {
	stackName, err := resolveStackNamed(skillPinsStack)
	if err != nil {
		return "", nil, nil, err
	}
	ps := skillpins.New(stackName)
	if err := ps.Load(); err != nil {
		return "", nil, nil, err
	}
	reg, err := loadRegistry()
	if err != nil {
		return "", nil, nil, err
	}
	return stackName, ps, reg, nil
}

// runningSkillPinsDaemon returns the daemon state when the resolved stack is
// running, or nil. Mutations route through the daemon's API when it runs, so
// its in-memory pin store stays authoritative; direct disk writes would be
// clobbered by the daemon's next sync.
func runningSkillPinsDaemon() *state.DaemonState {
	stackName, err := resolveStackNamed(skillPinsStack)
	if err != nil {
		return nil
	}
	st, err := state.Load(stackName)
	if err != nil || !state.IsRunning(st) {
		return nil
	}
	return st
}

// skillPinsListDoc is the machine-readable document for `skill pins list`.
type skillPinsListDoc struct {
	SchemaVersion int                            `json:"schema_version"`
	Stack         string                         `json:"stack"`
	Skills        map[string]*skillpins.SkillPin `json:"skills"`
}

func runSkillPinsList(format string) error {
	stackName, ps, _, err := loadSkillPinsForCLI()
	if err != nil {
		return err
	}
	skills := ps.GetAll()

	if strings.EqualFold(format, "json") {
		return output.EncodeJSON(os.Stdout, skillPinsListDoc{
			SchemaVersion: skillPinsJSONSchemaVersion,
			Stack:         stackName,
			Skills:        skills,
		})
	}

	if len(skills) == 0 {
		fmt.Printf("No skill pins found for stack '%s'. Start the stack to pin registry skills.\n", stackName)
		return nil
	}

	t := output.NewTableWriter(os.Stdout, *skillPinsListPlain)
	t.AppendHeader(table.Row{"SKILL", "STATUS", "SOURCE", "FILES", "FINDINGS", "LAST VERIFIED"})
	for _, name := range sortedMapKeys(skills) {
		pin := skills[name]
		t.AppendRow(table.Row{
			name,
			pinStatusLabel(pin.Status),
			pin.Source,
			len(pin.Files),
			len(pin.Findings),
			pin.LastVerifiedAt.Format("2006-01-02 15:04:05"),
		})
	}
	t.Render()
	return nil
}

// skillPinsVerifySkill is one skill's entry in the verify JSON document.
type skillPinsVerifySkill struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Source string `json:"source,omitempty"`
	Files  int    `json:"files"`
}

// skillPinsVerifyDoc is the machine-readable document for `skill pins verify`.
type skillPinsVerifyDoc struct {
	SchemaVersion int                    `json:"schema_version"`
	Stack         string                 `json:"stack"`
	HasDrift      bool                   `json:"has_drift"`
	Skills        []skillPinsVerifySkill `json:"skills"`
	// Missing lists pinned skills absent from the registry; their records
	// persist for review ('skill pins reset' clears them deliberately).
	Missing []string `json:"missing"`
}

func runSkillPinsVerify(stdout, stderr io.Writer, skill, format string, failOn string) int {
	stackName, ps, reg, err := loadSkillPinsForCLI()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return pinsExitInfrastructure
	}
	pinned := ps.GetAll()

	doc := skillPinsVerifyDoc{
		SchemaVersion: skillPinsJSONSchemaVersion,
		Stack:         stackName,
		Skills:        []skillPinsVerifySkill{},
		Missing:       []string{},
	}

	names := sortedMapKeys(pinned)
	if skill != "" {
		if _, ok := pinned[skill]; !ok {
			fmt.Fprintf(stderr, "no pin found for skill %q (see 'gridctl skill pins list')\n", skill)
			return pinsExitInfrastructure
		}
		names = []string{skill}
	}

	for _, name := range names {
		vr, err := ps.Verify(name, reg)
		switch {
		case errors.Is(err, skillpins.ErrDigestUnavailable):
			// Fail closed: unhashable content on a pinned skill is drift,
			// never "missing" (whose remedy — reset — would erase the pin
			// and silently re-pin whatever is there).
			fmt.Fprintf(stderr, "  ✗ %-24s content could not be hashed; treating as pin drift (%v)\n", name, err)
			doc.Skills = append(doc.Skills, skillPinsVerifySkill{Name: name, Status: skillpins.StatusDrift, Source: pinned[name].Source, Files: len(pinned[name].Files)})
			doc.HasDrift = true
			continue
		case errors.Is(err, registry.ErrNotFound):
			doc.Missing = append(doc.Missing, name)
			continue
		case err != nil:
			fmt.Fprintln(stderr, err)
			return pinsExitInfrastructure
		}
		entry := skillPinsVerifySkill{
			Name:   name,
			Status: vr.Status,
			Source: pinned[name].Source,
			Files:  len(pinned[name].Files),
		}
		doc.Skills = append(doc.Skills, entry)
		if vr.Status == skillpins.StatusDrift {
			doc.HasDrift = true
		}
	}

	if strings.EqualFold(format, "json") {
		if err := output.EncodeJSON(stdout, doc); err != nil {
			fmt.Fprintln(stderr, err)
			return pinsExitInfrastructure
		}
	} else {
		if len(doc.Skills) == 0 && len(doc.Missing) == 0 {
			fmt.Fprintf(stdout, "No skill pins found for stack '%s'. Start the stack to pin registry skills.\n", stackName)
			return pinsExitOK
		}
		for _, sv := range doc.Skills {
			if sv.Status == skillpins.StatusDrift {
				fmt.Fprintf(stdout, "  ✗ %-24s pin drift detected\n", sv.Name)
			} else {
				fmt.Fprintf(stdout, "  ✓ %-24s verified (%d supporting file(s))\n", sv.Name, sv.Files)
			}
		}
		for _, name := range doc.Missing {
			fmt.Fprintf(stdout, "  ? %-24s pinned but missing from the registry (run 'gridctl skill pins reset %s' to drop the record)\n", name, name)
		}
	}

	if doc.HasDrift {
		return pinsExitDrift
	}
	if failOn != "" && skillFindingsAtOrAbove(pinned, names, failOn) {
		fmt.Fprintf(stderr, "scan findings at or above %q present on pinned skills (see 'gridctl skill pins list')\n", failOn)
		return pinsExitDrift
	}
	return pinsExitOK
}

// skillFindingsAtOrAbove reports whether any of the named pins carries an
// advisory finding at or above the threshold severity.
func skillFindingsAtOrAbove(pinned map[string]*skillpins.SkillPin, names []string, threshold string) bool {
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

// skillPinsDiffDoc is the machine-readable document for `skill pins diff`.
type skillPinsDiffDoc struct {
	SchemaVersion   int            `json:"schema_version"`
	Stack           string         `json:"stack"`
	Skill           string         `json:"skill"`
	Status          string         `json:"status"`
	CompositeHash   string         `json:"composite_hash"`
	DocumentChanged bool           `json:"document_changed"`
	OldDocument     string         `json:"old_document,omitempty"`
	NewDocument     string         `json:"new_document,omitempty"`
	AddedFiles      []string       `json:"added_files"`
	RemovedFiles    []string       `json:"removed_files"`
	ModifiedFiles   []string       `json:"modified_files"`
	Findings        []pins.Finding `json:"findings"`
}

func runSkillPinsDiff(stdout, stderr io.Writer, skill, format, failOn string) int {
	stackName, ps, reg, err := loadSkillPinsForCLI()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return pinsExitInfrastructure
	}

	vr, err := ps.Verify(skill, reg)
	switch {
	case errors.Is(err, skillpins.ErrDigestUnavailable):
		fmt.Fprintf(stderr, "skill %q content could not be hashed; its pin fails closed as drift until the content is readable (%v)\n", skill, err)
		return pinsExitInfrastructure
	case errors.Is(err, registry.ErrNotFound):
		fmt.Fprintf(stderr, "skill %q not found in the registry (see 'gridctl skill pins list')\n", skill)
		return pinsExitInfrastructure
	case errors.Is(err, skillpins.ErrNotPinned):
		fmt.Fprintf(stderr, "no pin found for skill %q; it is pinned on the next registry refresh\n", skill)
		return pinsExitInfrastructure
	case err != nil:
		fmt.Fprintln(stderr, err)
		return pinsExitInfrastructure
	}

	doc := skillPinsDiffDoc{
		SchemaVersion: skillPinsJSONSchemaVersion,
		Stack:         stackName,
		Skill:         skill,
		Status:        vr.Status,
		CompositeHash: vr.CompositeHash,
		AddedFiles:    []string{},
		RemovedFiles:  []string{},
		ModifiedFiles: []string{},
		Findings:      []pins.Finding{},
	}
	if d := vr.Diff; d != nil {
		doc.DocumentChanged = d.DocumentChanged()
		doc.OldDocument = d.OldDocument
		doc.NewDocument = d.NewDocument
		doc.AddedFiles = d.AddedFiles
		doc.RemovedFiles = d.RemovedFiles
		doc.ModifiedFiles = d.ModifiedFiles
		doc.Findings = d.Findings
	}

	if strings.EqualFold(format, "json") {
		if err := output.EncodeJSON(stdout, doc); err != nil {
			fmt.Fprintln(stderr, err)
			return pinsExitInfrastructure
		}
	} else {
		renderSkillPinsDiffText(stdout, doc)
	}

	if doc.Status == skillpins.StatusDrift {
		return pinsExitDrift
	}
	pin, _ := ps.Get(skill)
	if failOn != "" && skillFindingsAtOrAbove(map[string]*skillpins.SkillPin{skill: pin}, []string{skill}, failOn) {
		fmt.Fprintf(stderr, "scan findings at or above %q present on this pinned skill (see 'gridctl skill pins list')\n", failOn)
		return pinsExitDrift
	}
	return pinsExitOK
}

// renderSkillPinsDiffText prints the human diff summary. Documents are large
// and prose-shaped; the text view summarizes what moved and points at the
// JSON output (or the Pins workspace) for the full before/after.
func renderSkillPinsDiffText(w io.Writer, doc skillPinsDiffDoc) {
	if doc.Status != skillpins.StatusDrift {
		fmt.Fprintf(w, "  ✓ %s verified — content matches its pin\n", doc.Skill)
		return
	}
	fmt.Fprintf(w, "  ✗ %s has pin drift\n", doc.Skill)
	if doc.DocumentChanged {
		fmt.Fprintf(w, "    SKILL.md changed (%d -> %d bytes canonical); full before/after in '--format json' or the Pins workspace\n",
			len(doc.OldDocument), len(doc.NewDocument))
	} else {
		fmt.Fprintln(w, "    SKILL.md unchanged")
	}
	for _, f := range doc.AddedFiles {
		fmt.Fprintf(w, "    + %s\n", escapeNonPrintable(f))
	}
	for _, f := range doc.RemovedFiles {
		fmt.Fprintf(w, "    - %s\n", escapeNonPrintable(f))
	}
	for _, f := range doc.ModifiedFiles {
		fmt.Fprintf(w, "    ~ %s\n", escapeNonPrintable(f))
	}
	for _, f := range doc.Findings {
		fmt.Fprintf(w, "    %s [%s/%s] %s: %s\n", findingGlyph(f.Severity), f.Code, f.Severity, f.Field, escapeNonPrintable(f.Message))
	}
	fmt.Fprintf(w, "    approve with: gridctl skill pins approve %s --expect %s\n", doc.Skill, doc.CompositeHash)
}

func runSkillPinsApprove(skill, expectHash, reason string) error {
	if st := runningSkillPinsDaemon(); st != nil {
		return approveSkillPinViaAPI(st, skill, expectHash, reason)
	}

	_, ps, reg, err := loadSkillPinsForCLI()
	if err != nil {
		return err
	}
	err = ps.Approve(skill, reg, expectHash, reason)
	switch {
	case errors.Is(err, skillpins.ErrDigestUnavailable):
		return fmt.Errorf("skill %q content could not be hashed; fix the unreadable file before approving (%v)", skill, err)
	case errors.Is(err, registry.ErrNotFound):
		return fmt.Errorf("skill %q not found in the registry (for a deleted skill, run 'gridctl skill pins reset %s')", skill, skill)
	case errors.Is(err, skillpins.ErrHashMismatch):
		return fmt.Errorf("skill content changed since the reviewed diff (run 'gridctl skill pins diff %s' to re-review)", skill)
	case errors.Is(err, skillpins.ErrReasonRequired):
		return fmt.Errorf("skill %q carries unresolved advisory findings; pass --reason to approve over them", skill)
	case err != nil:
		return err
	}
	fmt.Printf("✓ Approved content update for skill %s\n", skill)
	return nil
}

func approveSkillPinViaAPI(st *state.DaemonState, skill, expectHash, reason string) error {
	payload, err := json.Marshal(map[string]string{"expected_hash": expectHash, "reason": reason})
	if err != nil {
		return fmt.Errorf("encoding request: %w", err)
	}
	url := fmt.Sprintf("http://localhost:%d/api/skill-pins/%s/approve", st.Port, skill)
	client := &http.Client{Timeout: pinsAPITimeout}
	resp, err := client.Post(url, "application/json", strings.NewReader(string(payload)))
	if err != nil {
		return fmt.Errorf("calling skill pins API: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading response: %w", err)
	}
	switch resp.StatusCode {
	case http.StatusOK:
		fmt.Printf("✓ Approved content update for skill %s\n", skill)
		return nil
	case http.StatusNotFound:
		return fmt.Errorf("%s (for a deleted skill, run 'gridctl skill pins reset %s')",
			apiErrorMessage(body, "skill not found: "+skill), skill)
	case http.StatusConflict:
		return fmt.Errorf("%s (run 'gridctl skill pins diff %s' to re-review)",
			apiErrorMessage(body, "skill content changed since review"), skill)
	case http.StatusBadRequest:
		return fmt.Errorf("%s", apiErrorMessage(body, "approve rejected"))
	default:
		return fmt.Errorf("approve failed: %s", string(body))
	}
}

func runSkillPinsReset(skill string) error {
	if st := runningSkillPinsDaemon(); st != nil {
		return resetSkillPinViaAPI(st, skill)
	}

	_, ps, _, err := loadSkillPinsForCLI()
	if err != nil {
		return err
	}
	if _, ok := ps.Get(skill); !ok {
		return fmt.Errorf("no pin found for skill %q (see 'gridctl skill pins list')", skill)
	}
	if err := ps.Reset(skill); err != nil {
		return err
	}
	fmt.Printf("✓ Pin reset for skill %s. It is re-pinned on the next registry refresh.\n", skill)
	return nil
}

func resetSkillPinViaAPI(st *state.DaemonState, skill string) error {
	url := fmt.Sprintf("http://localhost:%d/api/skill-pins/%s", st.Port, skill)
	client := &http.Client{Timeout: pinsAPITimeout}
	req, err := http.NewRequest(http.MethodDelete, url, nil)
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("calling skill pins API: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading response: %w", err)
	}
	if resp.StatusCode == http.StatusNotFound {
		return fmt.Errorf("no pin found for skill %q (see 'gridctl skill pins list')", skill)
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("reset failed: %s", string(body))
	}
	fmt.Printf("✓ Pin reset for skill %s. It is re-pinned on the next registry refresh.\n", skill)
	return nil
}
