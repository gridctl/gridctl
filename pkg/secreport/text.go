package secreport

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/gridctl/gridctl/pkg/output"
)

func WriteJSON(w io.Writer, report *Report) error {
	return output.EncodeJSON(w, report)
}

func WriteText(w io.Writer, report *Report, quiet bool) {
	fmt.Fprintf(w, "Security evidence report\n")
	fmt.Fprintf(w, "Source: %s %s\n", report.Source.Kind, report.Source.Display)
	if report.Source.Historical {
		fmt.Fprintf(w, "Historical: supplied snapshot; not a fresh verification\n")
	}
	fmt.Fprintf(w, "Generated: %s\n", report.GeneratedAt.UTC().Format(time.RFC3339))
	fmt.Fprintf(w, "Coverage: %s (%d unknown gaps among %d inventory predicates)\n", report.Coverage.Status, report.Coverage.UnknownGaps, report.Coverage.PredicatesTotal)
	fmt.Fprintln(w)
	for _, c := range report.Checks {
		if quiet && c.Outcome != OutcomeFail && c.Outcome != OutcomeWarn && c.Outcome != OutcomeUnknown {
			continue
		}
		fmt.Fprintf(w, "  %-14s %-28s %s/%s  %s\n", c.Outcome, c.Predicate, c.Subject.Kind, c.Subject.Name, c.Explanation)
		fmt.Fprintf(w, "                 basis=%s availability=%s freshness=%s\n", c.Evidence.Basis, c.Evidence.Availability, c.Evidence.Freshness)
		if len(c.Limitations) > 0 {
			fmt.Fprintf(w, "                 limitation: %s\n", strings.Join(c.Limitations, "; "))
		}
	}
	fmt.Fprintf(w, "\nResult: %d failure(s), %d warning(s), %d unknown; coverage %s. Exit zero is not secure.\n", report.FailCount, report.WarnCount, report.UnknownCount, report.Coverage.Status)
	fmt.Fprintln(w, QuietSummary(report))
}

func QuietSummary(report *Report) string {
	if report.FailCount == 0 {
		return "No failing checks; coverage " + report.Coverage.Status + "."
	}
	return fmt.Sprintf("%d failing check(s); coverage %s.", report.FailCount, report.Coverage.Status)
}
