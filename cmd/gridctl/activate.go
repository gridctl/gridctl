package main

import (
	"fmt"
	"io"
	"os"

	"github.com/gridctl/gridctl/pkg/registry"
	"github.com/spf13/cobra"
)

var activateCmd = &cobra.Command{
	Use:   "activate <skill-name>",
	Short: "Activate a skill in the registry",
	Long: `Transition a skill from draft to active state.

Executable skills (those with a workflow) must have acceptance criteria
defined before they can be activated. At least one criterion must match
the GIVEN <context> WHEN <action> THEN <assertion> format.

Exit codes:
  0  Skill activated successfully
  1  Activation failed (missing or malformed acceptance criteria)`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runActivate(args[0])
	},
}

func runActivate(skillName string) error {
	store, err := loadRegistry()
	if err != nil {
		return err
	}

	sk, err := store.GetSkill(skillName)
	if err != nil {
		return fmt.Errorf("skill %q not found in registry", skillName)
	}

	if sk.IsExecutable() && len(sk.AcceptanceCriteria) == 0 {
		fmt.Fprintf(os.Stderr, "✗ cannot activate %q: executable skill has no acceptance criteria\n", skillName)
		fmt.Fprintf(os.Stderr, "  Add acceptance_criteria to the skill frontmatter:\n\n")
		fmt.Fprintf(os.Stderr, "  acceptance_criteria:\n")
		fmt.Fprintf(os.Stderr, "    - GIVEN <context> WHEN the skill is called THEN <assertion>\n\n")
		fmt.Fprintf(os.Stderr, "  Then run: gridctl activate %s\n", skillName)
		os.Exit(1)
	}

	if sk.IsExecutable() {
		parseable := countParseableCriteria(sk.AcceptanceCriteria)
		if parseable == 0 {
			printMalformedCriteriaError(os.Stderr, skillName, sk.AcceptanceCriteria)
			os.Exit(1)
		}
	}

	sk.State = registry.StateActive
	if err := store.SaveSkill(sk); err != nil {
		return fmt.Errorf("saving skill: %w", err)
	}

	fmt.Printf("✓ Skill %q activated\n", skillName)
	return nil
}

// countParseableCriteria returns the number of criteria that match GIVEN ... WHEN ... THEN.
func countParseableCriteria(criteria []string) int {
	n := 0
	for _, c := range criteria {
		if registry.ParseCriterion(c) != nil {
			n++
		}
	}
	return n
}

// printMalformedCriteriaError writes the per-criterion error report to w.
func printMalformedCriteriaError(w io.Writer, skillName string, criteria []string) {
	fmt.Fprintf(w, "✗ cannot activate %q: 0 of %d criteria match GIVEN ... WHEN ... THEN\n\n", skillName, len(criteria))
	for i, c := range criteria {
		fmt.Fprintf(w, "  [%d] ✗  %s\n", i+1, c)
		fmt.Fprintf(w, "         does not match GIVEN ... WHEN ... THEN\n")
	}
	fmt.Fprintf(w, "\n  Fix the malformed criteria and re-run: gridctl activate %s\n", skillName)
}
