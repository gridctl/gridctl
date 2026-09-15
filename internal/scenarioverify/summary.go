package scenarioverify

import (
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strings"
)

type summaryFile struct {
	Lane           string              `json:"lane"`
	Revision       string              `json:"revision"`
	GoStatus       int                 `json:"go_status"`
	CaptureStatus  int                 `json:"capture_status"`
	VerifierStatus int                 `json:"verifier_status"`
	Required       string              `json:"required"`
	LaneReason     string              `json:"lane_reason,omitempty"`
	Scenarios      []summaryScenario   `json:"scenarios"`
}

type summaryScenario struct {
	ID       string `json:"id"`
	Package  string `json:"package"`
	Test     string `json:"test"`
	Status   string `json:"status"`
	Reason   string `json:"reason,omitempty"`
	Boundary string `json:"expected_boundary"`
	Observed string `json:"observed"`
}

func WriteText(w io.Writer, r *Report) error {
	if _, err := fmt.Fprintf(w, "Required scenarios: %s\n", r.Required); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "Lane: %s\n", r.Lane); err != nil {
		return err
	}
	if r.Revision != "" {
		if _, err := fmt.Fprintf(w, "Revision: %s\n", r.Revision); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(w, "Go status: %d\nCapture status: %d\nVerifier status: %d\n", r.GoStatus, r.CaptureStatus, r.VerifierStatus); err != nil {
		return err
	}
	for _, sc := range r.Scenarios {
		if sc.Status == "pass" {
			if _, err := fmt.Fprintf(w, "PASS %s\n", sc.ID); err != nil {
				return err
			}
			continue
		}
		reason := sc.Reason
		if reason == "" {
			reason = r.LaneReason
		}
		if _, err := fmt.Fprintf(w, "FAIL %s reason=%s\n", sc.ID, reason); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(w, "Expected boundary: %s\n", sc.Boundary); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(w, "Observed: %s\n", sc.Observed); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(w, "Next: inspect earlier suite failure, build tags, and selector drift\n"); err != nil {
			return err
		}
	}
	return nil
}

func WriteJSON(w io.Writer, r *Report) error {
	out := summaryFile{
		Lane:           r.Lane,
		Revision:       r.Revision,
		GoStatus:       r.GoStatus,
		CaptureStatus:  r.CaptureStatus,
		VerifierStatus: r.VerifierStatus,
		Required:       r.Required,
		LaneReason:     r.LaneReason,
	}
	for _, sc := range r.Scenarios {
		out.Scenarios = append(out.Scenarios, summaryScenario(sc))
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}

func FocusedCommand(sc Scenario) string {
	parts := strings.Split(sc.Test, "/")
	escaped := make([]string, 0, len(parts))
	for _, part := range parts {
		escaped = append(escaped, "^"+regexp.QuoteMeta(part)+"$")
	}
	pkgDir := strings.TrimPrefix(sc.Package, "github.com/gridctl/gridctl/")
	if pkgDir == sc.Package {
		pkgDir = sc.Package
	} else {
		pkgDir = "./" + pkgDir
	}
	return "go test -race -count=1 -run '" + strings.Join(escaped, "/") + "' " + pkgDir
}


