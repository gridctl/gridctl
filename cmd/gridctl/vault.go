package main

import (
	"log/slog"
	"strings"
	"sync"

	"github.com/spf13/cobra"
)

// vaultDeprecationOnce ensures the deprecation banner prints at most once per
// process (Article XIV: structured logging; no per-invocation spam).
var vaultDeprecationOnce sync.Once

// warnVaultDeprecated emits the deprecation banner on first use of any
// `gridctl vault ...` subcommand within the process. Subsequent invocations
// in the same process pass silently.
func warnVaultDeprecated() {
	vaultDeprecationOnce.Do(func() {
		slog.Warn(`"gridctl vault" is deprecated. Use "gridctl var" instead. ` +
			`The old command will be removed at v1.0. See "gridctl var --help".`)
	})
}

// vaultCmd is a thin redirector that hands every vault subcommand off to the
// equivalent under `gridctl var`. The implementation lives in var.go; this
// file exists only to keep existing scripts and muscle memory working through
// the beta cycle.
var vaultCmd = &cobra.Command{
	Use:     "vault",
	Short:   "[deprecated] Manage secrets in the gridctl vault (use 'gridctl var')",
	Long:    `Deprecated alias for 'gridctl var'. See 'gridctl var --help' for full usage.`,
	Hidden:  true,
	Aliases: []string{},
}

func init() {
	// Mirror every var subcommand so `gridctl vault <sub>` and
	// `gridctl var <sub>` parse identically. cobra requires distinct command
	// instances per parent, so we build deprecation wrappers that delegate
	// to the same RunE bodies as the var tree.
	for _, sub := range cloneSubcommandsForDeprecation(varCmd.Commands()) {
		vaultCmd.AddCommand(sub)
	}
}

// cloneSubcommandsForDeprecation returns deep-ish copies of the var-tree
// commands suitable for hanging off `vault`. Each copy wraps the original
// RunE so that any invocation under `vault` emits the once-per-process
// deprecation warning before delegating.
func cloneSubcommandsForDeprecation(originals []*cobra.Command) []*cobra.Command {
	out := make([]*cobra.Command, 0, len(originals))
	for _, orig := range originals {
		out = append(out, wrapForDeprecation(orig))
	}
	return out
}

func wrapForDeprecation(orig *cobra.Command) *cobra.Command {
	cp := &cobra.Command{
		Use:     orig.Use,
		Short:   orig.Short,
		Long:    orig.Long,
		Args:    orig.Args,
		Aliases: append([]string{}, orig.Aliases...),
		Hidden:  true,
	}

	// Re-attach exactly the same flags. cobra's flag set is identified by
	// pointer, so this shares the var-tree's flag variables — running
	// `gridctl vault set FOO --plaintext` flips the same `varSetPlaintext`
	// var that the canonical command reads.
	cp.Flags().AddFlagSet(orig.Flags())

	if orig.RunE != nil {
		wrappedRunE := orig.RunE
		cp.RunE = func(cmd *cobra.Command, args []string) error {
			warnVaultDeprecated()
			// Rewrite the help/usage hint to point at the new command tree.
			return wrappedRunE(cmd, rewriteArgs(args))
		}
	}

	for _, child := range orig.Commands() {
		cp.AddCommand(wrapForDeprecation(child))
	}

	return cp
}

// rewriteArgs is a hook for future use; today it's a no-op. Kept so the
// deprecation wrapper signature reads symmetrically with the var-tree.
func rewriteArgs(args []string) []string {
	return args
}

// stripVaultPrefix is unused at runtime but documents an invariant: the
// vault tree should never reach the var-tree carrying its own command-line
// prefix. If a future refactor introduces re-dispatch, this is the seam.
var _ = strings.HasPrefix
