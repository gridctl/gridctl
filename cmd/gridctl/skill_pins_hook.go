package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/gridctl/gridctl/pkg/hookpins"
	"github.com/gridctl/gridctl/pkg/output"
	"github.com/gridctl/gridctl/pkg/pinapprovals"
	"github.com/gridctl/gridctl/pkg/pins"
	"github.com/gridctl/gridctl/pkg/skills"

	"github.com/jedib0t/go-pretty/v6/table"
)

// prepareSkillPinsKind rejects an unknown kind, and rejects --stack with
// --kind hook before any stack is resolved.
func prepareSkillPinsKind() error {
	switch skillPinsKind {
	case "", skillProjectKindSkill, skillKindHook:
	default:
		return fmt.Errorf("unknown --kind %q (supported: skill, hook)", skillPinsKind)
	}
	if skillPinsKind == skillKindHook && skillPinsStack != "" {
		return errHookPinsStack
	}
	return nil
}

func loadHookPins() (*hookpins.Store, error) {
	ps := hookpins.New()
	if err := ps.Load(); err != nil {
		return nil, err
	}
	return ps, nil
}

type hookPinsListDoc struct {
	SchemaVersion int                          `json:"schema_version"`
	Hooks         map[string]*hookpins.HookPin `json:"hooks"`
}

func runHookPinsList(format string) error {
	ps, err := loadHookPins()
	if err != nil {
		return err
	}
	if _, err := ps.Sync(context.Background(), registryDir()); err != nil {
		return err
	}
	hooks := ps.Pins()
	if strings.EqualFold(format, "json") {
		if hooks == nil {
			hooks = map[string]*hookpins.HookPin{}
		}
		return output.EncodeJSON(os.Stdout, hookPinsListDoc{
			SchemaVersion: skillPinsJSONSchemaVersion,
			Hooks:         hooks,
		})
	}
	if len(hooks) == 0 {
		fmt.Println("No hook pins found.")
		return nil
	}
	t := output.NewTableWriter(os.Stdout, *skillPinsListPlain)
	t.AppendHeader(table.Row{"HOOK", "STATUS", "SOURCE", "FILES", "FINDINGS", "LAST VERIFIED"})
	for _, name := range sortedMapKeysHook(hooks) {
		pin := hooks[name]
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

func sortedMapKeysHook(m map[string]*hookpins.HookPin) []string {
	names := make([]string, 0, len(m))
	for name := range m {
		names = append(names, name)
	}
	sortStrings(names)
	return names
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

type hookPinsVerifyDoc struct {
	SchemaVersion int    `json:"schema_version"`
	HasDrift      bool   `json:"has_drift"`
	Hooks         []hookPinsVerifyHook `json:"hooks"`
	Missing       []string `json:"missing"`
}

type hookPinsVerifyHook struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Source string `json:"source,omitempty"`
	Files  int    `json:"files"`
}

func runHookPinsVerify(stdout, stderr io.Writer, name, format, failOn string) int {
	ps, err := loadHookPins()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return pinsExitInfrastructure
	}
	pinned := ps.Pins()
	doc := hookPinsVerifyDoc{SchemaVersion: skillPinsJSONSchemaVersion, Hooks: []hookPinsVerifyHook{}, Missing: []string{}}
	names := sortedMapKeysHook(pinned)
	if name != "" {
		if _, ok := pinned[name]; !ok {
			fmt.Fprintf(stderr, "hook %q is not pinned\n", name)
			return pinsExitInfrastructure
		}
		names = []string{name}
	}
	drift := false
	for _, n := range names {
		vr, verr := ps.Verify(n, registryDir())
		if verr != nil {
			if errors.Is(verr, hookpins.ErrNotFound) {
				doc.Missing = append(doc.Missing, n)
				continue
			}
			fmt.Fprintln(stderr, verr)
			return pinsExitInfrastructure
		}
		if vr.Status == hookpins.StatusDrift {
			drift = true
		}
		doc.Hooks = append(doc.Hooks, hookPinsVerifyHook{Name: n, Status: vr.Status, Source: pinned[n].Source, Files: len(pinned[n].Files)})
	}
	doc.HasDrift = drift
	if strings.EqualFold(format, "json") {
		if err := output.EncodeJSON(stdout, doc); err != nil {
			fmt.Fprintln(stderr, err)
			return pinsExitInfrastructure
		}
	} else if drift {
		fmt.Fprintln(stdout, "pin drift detected")
		for _, h := range doc.Hooks {
			if h.Status == hookpins.StatusDrift {
				fmt.Fprintf(stdout, "  %s\n", h.Name)
			}
		}
	} else {
		fmt.Fprintln(stdout, "hook pins verified")
	}
	if drift {
		return pinsExitDrift
	}
	if failOn != "" && hookFindingsAtOrAbove(pinned, names, failOn) {
		return pinsExitDrift
	}
	return pinsExitOK
}

func runHookPinsDiff(stdout, stderr io.Writer, name, format, failOn string) int {
	ps, err := loadHookPins()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return pinsExitInfrastructure
	}
	vr, err := ps.Diff(name, registryDir())
	if err != nil {
		fmt.Fprintln(stderr, err)
		return pinsExitInfrastructure
	}
	doc := map[string]any{
		"schema_version": skillPinsJSONSchemaVersion,
		"hook":           name,
		"status":         vr.Status,
		"composite_hash": vr.CompositeHash,
	}
	if vr.Diff != nil {
		doc["diff"] = vr.Diff
	}
	if strings.EqualFold(format, "json") {
		if err := output.EncodeJSON(stdout, doc); err != nil {
			fmt.Fprintln(stderr, err)
			return pinsExitInfrastructure
		}
	} else if vr.Status == hookpins.StatusDrift && vr.Diff != nil {
		fmt.Fprintf(stdout, "%s pin drift\n", name)
		fmt.Fprintf(stdout, "  definition: %s -> %s\n", vr.Diff.OldHash, vr.Diff.NewHash)
		fmt.Fprintf(stdout, "  approve with: gridctl skill pins approve %s --kind hook --expect %s --reason \"<why>\"\n", name, vr.CompositeHash)
	} else {
		fmt.Fprintf(stdout, "%s pin is current\n", name)
	}
	if vr.Status == hookpins.StatusDrift {
		return pinsExitDrift
	}
	pin, _ := ps.Get(name)
	if failOn != "" && pin != nil && hookFindingsAtOrAbove(map[string]*hookpins.HookPin{name: pin}, []string{name}, failOn) {
		return pinsExitDrift
	}
	return pinsExitOK
}

func runHookPinsApprove(ctx context.Context, name, expectHash, reason string, evidence []string) error {
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
	ps, err := loadHookPins()
	if err != nil {
		return err
	}
	findings := []pins.Finding{}
	if hook, herr := skills.GetHook(registryDir(), name); herr == nil {
		scan := skills.ScanHook(name, hook.Definition.Raw, hook.Dir)
		findings = hookScanFindings(scan)
	}
	err = ps.Approve(ctx, name, registryDir(), hookpins.ApproveRequest{
		ExpectedHash:     expectHash,
		Reason:           reason,
		Evidence:         cleaned,
		ApprovedBy:       by,
		ApprovedBySource: source,
		Findings:         findings,
	})
	switch {
	case errors.Is(err, hookpins.ErrHashMismatch):
		return fmt.Errorf("hook content changed since the reviewed diff (run 'gridctl skill pins diff %s --kind hook' to re-review)", name)
	case errors.Is(err, hookpins.ErrReasonRequired):
		return fmt.Errorf("hook %q carries unresolved findings; pass --reason to approve over them", name)
	case errors.Is(err, hookpins.ErrNotPinned):
		return fmt.Errorf("hook %q is not pinned", name)
	case err != nil:
		return err
	}
	fmt.Printf("Approved content update for hook %s\n", name)
	return nil
}

func runHookPinsReset(ctx context.Context, name string) error {
	ps, err := loadHookPins()
	if err != nil {
		return err
	}
	if err := ps.Reset(ctx, name); err != nil {
		return err
	}
	fmt.Printf("Reset pin for hook %s\n", name)
	return nil
}

func hookFindingsAtOrAbove(pinned map[string]*hookpins.HookPin, names []string, threshold string) bool {
	for _, name := range names {
		pin := pinned[name]
		if pin == nil {
			continue
		}
		for _, f := range pin.Findings {
			if findingMeets(f.Severity, threshold) {
				return true
			}
		}
	}
	return false
}

func findingMeets(severity, threshold string) bool {
	rank := func(s string) int {
		switch s {
		case pins.SeverityCritical, "danger":
			return 2
		case pins.SeverityWarn, "warning":
			return 1
		default:
			return 0
		}
	}
	return rank(severity) >= rank(threshold)
}

func hookScanFindings(scan *skills.ScanResult) []pins.Finding {
	if scan == nil || len(scan.Findings) == 0 {
		return nil
	}
	out := make([]pins.Finding, 0, len(scan.Findings))
	for _, f := range scan.Findings {
		sev := pins.SeverityWarn
		if f.Severity == "danger" {
			sev = pins.SeverityCritical
		}
		out = append(out, pins.Finding{Code: "hook-scan", Severity: sev, Field: f.StepID, Message: f.Description, Snippet: f.Pattern})
	}
	return out
}
