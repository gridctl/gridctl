package main

import (
	"fmt"
	"os"

	"github.com/gridctl/gridctl/pkg/registry"
	"github.com/spf13/cobra"
)

var activateCmd = &cobra.Command{
	Use:   "activate <skill-name>",
	Short: "Activate a skill in the registry",
	Long: `Transition a skill from draft to active state.

Executable skills (those with a workflow) must have acceptance criteria
defined before they can be activated. Add acceptance_criteria to the
skill's SKILL.md frontmatter to satisfy this requirement.

Exit codes:
  0  Skill activated successfully
  1  Activation failed (missing acceptance criteria or validation error)`,
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

	sk.State = registry.StateActive
	if err := store.SaveSkill(sk); err != nil {
		return fmt.Errorf("saving skill: %w", err)
	}

	fmt.Printf("✓ Skill %q activated\n", skillName)
	return nil
}
