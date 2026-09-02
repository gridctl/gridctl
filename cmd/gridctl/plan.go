package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/gridctl/gridctl/pkg/builder"
	"github.com/gridctl/gridctl/pkg/config"
	"github.com/gridctl/gridctl/pkg/controller"
	gitpkg "github.com/gridctl/gridctl/pkg/git"
	"github.com/gridctl/gridctl/pkg/output"
	"github.com/gridctl/gridctl/pkg/provisioner"
	"github.com/gridctl/gridctl/pkg/runtime"
	runtimedocker "github.com/gridctl/gridctl/pkg/runtime/docker"
	"github.com/gridctl/gridctl/pkg/state"

	"github.com/spf13/cobra"
)

var (
	planAutoApprove    bool
	planAutoApproveCI  bool
	planFormat         string
	planShowDockerfile bool
)

var planCmd = &cobra.Command{
	Use:   "plan [stack.yaml]",
	Short: "Compare stack spec against running state",
	Long: `Loads the stack specification and compares it against the currently
running deployment. Shows a structured diff of what would change:
added, removed, and modified servers, agents, and resources.

Source builds include their resolved identity, image tag, and cache state.
Use --show-dockerfile to inspect generated Python Dockerfiles.

Use -y or --auto-approve to auto-approve and apply changes.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		var err error
		if planFormat, err = resolveFormat(planFormat, cmd.Flags().Changed("format"), *planJSON); err != nil {
			return err
		}
		return runPlan(cmd.Context(), args[0])
	},
}

var planJSON *bool

func init() {
	planCmd.Flags().BoolVarP(&planAutoApprove, "yes", "y", false, "Auto-approve and apply changes")
	planCmd.Flags().BoolVar(&planAutoApproveCI, "auto-approve", false, "Auto-approve and apply changes (CI/CD equivalent of -y)")
	planCmd.Flags().StringVar(&planFormat, "format", "", "Output format: json for machine-readable output")
	planCmd.Flags().BoolVar(&planShowDockerfile, "show-dockerfile", false, "Show generated Python Dockerfiles")
	planJSON = addJSONAlias(planCmd)
}

func runPlan(ctx context.Context, stackPath string) error {
	// Load and validate the proposed spec
	proposed, result, err := config.ValidateStackFile(stackPath)
	if err != nil {
		return fmt.Errorf("loading proposed spec: %w", err)
	}
	if result.ErrorCount > 0 {
		printValidationResult(stackPath, result)
		return fmt.Errorf("proposed spec has %d validation error(s)", result.ErrorCount)
	}
	indexed, err := config.ParseStackIndex(ctx, stackPath)
	if err != nil {
		return fmt.Errorf("indexing stack declarations: %w", err)
	}
	diagnostics := declarationDiagnostics(indexed)
	appendDeclarationValidationIssues(result, diagnostics)

	// Find the running stack's state
	current, err := loadCurrentStack(proposed.Name)
	if err != nil {
		return err
	}

	// Compute the diff
	diff := config.ComputePlan(proposed, current)
	diff.VariableDiagnostics = diagnostics
	diff.Builds, err = resolvePlanBuilds(ctx, proposed, planShowDockerfile)
	buildErr := err

	// Declared client links are host-only work, kept out of PlanDiff.Items
	// so the container/gateway summary never claims link changes.
	var links []linkAction
	if len(proposed.Link) > 0 {
		links = computeLinkActions(provisioner.NewRegistry(), proposed.Link)
	}

	if planFormat == "json" {
		if err := output.EncodeJSON(os.Stdout, struct {
			*config.PlanDiff
			ClientLinks []linkAction `json:"clientLinks,omitempty"`
		}{diff, links}); err != nil {
			return err
		}
		if buildErr != nil {
			return fmt.Errorf("resolving source build plan: %w", buildErr)
		}
		return nil
	}

	printPlanDiff(os.Stdout, diff)
	printPlanBuilds(os.Stdout, diff.Builds, planShowDockerfile)
	for _, diagnostic := range diagnostics {
		fmt.Printf("! variable %s: %s\n", diagnostic.Key, diagnostic.Message)
	}
	printLinkActions(os.Stdout, links)
	if buildErr != nil {
		return fmt.Errorf("resolving source build plan: %w", buildErr)
	}

	if !diff.HasChanges {
		return nil
	}

	// Confirm or auto-approve
	if !planAutoApprove && !planAutoApproveCI {
		fmt.Print("\nApply these changes? [y/N] ")
		reader := bufio.NewReader(os.Stdin)
		answer, _ := reader.ReadString('\n')
		answer = strings.TrimSpace(strings.ToLower(answer))
		if answer != "y" && answer != "yes" {
			fmt.Println("Cancelled.")
			return nil
		}
	}

	// Apply with Replace to handle running stacks
	fmt.Println("\nApplying changes...")
	ctrl := controller.New(controller.Config{
		StackPath:  stackPath,
		Port:       applyPort,
		BasePort:   applyBasePort,
		Foreground: applyForeground,
		Runtime:    runtimeFlag,
		Replace:    true,
		LogLevel:   logLevel,
	})
	ctrl.SetVersion(version)
	ctrl.SetWebFS(WebFS)

	return ctrl.Deploy(context.Background())
}

func resolvePlanBuilds(ctx context.Context, stack *config.Stack, showDockerfile bool) ([]config.BuildAction, error) {
	b := builder.New(nil)
	cacheAvailable := false
	var closeClient func() error
	if info, err := runtime.DetectRuntime(runtime.DetectOptions{Explicit: runtimeFlag}); err == nil {
		if dr, err := runtimedocker.NewWithInfo(info); err == nil {
			pingCtx, cancel := context.WithTimeout(ctx, time.Second)
			_, pingErr := dr.Client().Ping(pingCtx)
			cancel()
			if pingErr == nil {
				b = builder.New(dr.Client())
				cacheAvailable = true
				closeClient = dr.Client().Close
			} else {
				_ = dr.Client().Close()
			}
		}
	}
	if closeClient != nil {
		defer func() { _ = closeClient() }()
	}

	var actions []config.BuildAction
	var resolutionErrors []error
	for i := range stack.MCPServers {
		server := &stack.MCPServers[i]
		if server.Source == nil {
			continue
		}
		opts := buildOptionsForServer(stack.Name, server)
		action := config.BuildAction{
			Server: server.Name, SourceType: server.Source.Type,
			DeclaredIdentity: declaredPlanIdentity(server.Source),
			CacheState:       "unknown",
		}
		if server.Source.Auth != nil {
			auth, err := runtime.AuthForSource(server.Source.Auth, server.Source.URL, cliCredentialResolver)
			if err != nil {
				action.Error = redactPlanError(err, server.Source.URL)
				actions = append(actions, action)
				resolutionErrors = append(resolutionErrors, fmt.Errorf("%s: %s", server.Name, action.Error))
				continue
			}
			opts.Auth = auth
		}
		var plan *builder.ResolvedBuildPlan
		var err error
		if cacheAvailable {
			plan, err = b.Plan(ctx, opts)
		} else {
			plan, err = b.Resolve(ctx, opts)
		}
		if err != nil {
			action.Error = redactPlanError(err, server.Source.URL)
			actions = append(actions, action)
			resolutionErrors = append(resolutionErrors, fmt.Errorf("%s: %s", server.Name, action.Error))
			continue
		}
		action.DeclaredIdentity = planIdentity(plan.DeclaredIdentity)
		action.ResolvedIdentity = planIdentity(plan.ResolvedIdentity)
		action.ImageTag = plan.ImageTag
		action.Cached = plan.Cached
		action.MutableRef = plan.MutableRef
		if cacheAvailable {
			action.CacheState = "build"
			if plan.Cached {
				action.CacheState = "cached"
			}
		}
		if showDockerfile {
			action.GeneratedDockerfile = plan.GeneratedDockerfile
		}
		if err := plan.Close(); err != nil {
			action.Error = fmt.Sprintf("cleaning build plan: %v", err)
			resolutionErrors = append(resolutionErrors, fmt.Errorf("%s: cleaning build plan: %w", server.Name, err))
		}
		actions = append(actions, action)
	}
	return actions, errors.Join(resolutionErrors...)
}

func buildOptionsForServer(stackName string, server *config.MCPServer) builder.BuildOptions {
	source := server.Source
	return builder.BuildOptions{
		Stack: stackName, ServerName: server.Name, SourceType: source.Type,
		URL: source.URL, Ref: source.Ref, Path: source.Path, ProjectPath: source.ProjectPath,
		Runtime: source.Runtime, Package: source.Package, Python: source.Python,
		Extras: source.Extras, With: source.With, Packages: source.Packages,
		Dockerfile: source.Dockerfile, BuildArgs: server.BuildArgs, Command: server.Command,
	}
}

func planIdentity(identity builder.SourceIdentity) config.BuildIdentity {
	return config.BuildIdentity{
		Type: identity.Type, URL: sanitizePlanURL(identity.URL), Ref: identity.Ref, Path: identity.Path,
		ProjectPath: identity.ProjectPath, Dockerfile: identity.Dockerfile, Commit: identity.Commit,
		Package: identity.Package, Version: identity.Version, Artifact: identity.Artifact,
		ArtifactSHA256: identity.ArtifactSHA256,
	}
}

func declaredPlanIdentity(source *config.Source) config.BuildIdentity {
	return config.BuildIdentity{
		Type: source.Type, URL: sanitizePlanURL(source.URL), Ref: source.Ref,
		Path: source.Path, ProjectPath: source.ProjectPath, Dockerfile: source.Dockerfile,
		Package: source.Package,
	}
}

func sanitizePlanURL(value string) string {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme == "" {
		return gitpkg.RedactURL(value)
	}
	parsed.User = nil
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String()
}

func redactPlanError(err error, sourceURL string) string {
	message := err.Error()
	if sourceURL != "" {
		message = strings.ReplaceAll(message, sourceURL, sanitizePlanURL(sourceURL))
	}
	return gitpkg.RedactString(message)
}

func printPlanBuilds(w io.Writer, builds []config.BuildAction, showDockerfile bool) {
	if len(builds) == 0 {
		return
	}
	fmt.Fprintln(w, "\nSource builds:")
	for _, build := range builds {
		resolved := build.ResolvedIdentity
		if resolved.Type == "" && resolved.URL == "" && resolved.Ref == "" && resolved.Path == "" &&
			resolved.Commit == "" && resolved.Package == "" && resolved.Version == "" {
			resolved = build.DeclaredIdentity
		}
		identity := build.SourceType
		if resolved.Package != "" {
			version := resolved.Version
			if version == "" {
				version = resolved.Ref
			}
			identity += ":" + resolved.Package + "==" + version
		} else if resolved.Commit != "" {
			identity += ":" + resolved.Commit
		} else if resolved.Path != "" {
			identity += ":" + resolved.Path
		}
		if build.ImageTag == "" {
			fmt.Fprintf(w, "  + %s (build %s, cache: %s)\n", build.Server, identity, build.CacheState)
		} else {
			fmt.Fprintf(w, "  + %s (build %s -> %s, cache: %s)\n", build.Server, identity, build.ImageTag, build.CacheState)
		}
		if build.Error != "" {
			fmt.Fprintf(w, "      error: %s\n", build.Error)
		}
		if build.MutableRef {
			fmt.Fprintln(w, "      warning: declared git ref is mutable")
		}
		if showDockerfile && build.GeneratedDockerfile != "" {
			fmt.Fprintf(w, "\n--- %s generated Dockerfile ---\n%s", build.Server, build.GeneratedDockerfile)
			if !strings.HasSuffix(build.GeneratedDockerfile, "\n") {
				fmt.Fprintln(w)
			}
		}
	}
}

// loadCurrentStack finds and loads the currently running stack config.
func loadCurrentStack(stackName string) (*config.Stack, error) {
	st, err := state.Load(stackName)
	if err != nil {
		if os.IsNotExist(err) {
			// No running stack — everything is an add
			return &config.Stack{Name: stackName}, nil
		}
		return nil, fmt.Errorf("loading state for %q: %w", stackName, err)
	}

	if !state.IsRunning(st) {
		// Stale state — treat as no running stack
		return &config.Stack{Name: stackName}, nil
	}

	// Load the running stack's config
	current, _, parseErr := config.ValidateStackFile(st.StackFile)
	if parseErr != nil {
		return nil, fmt.Errorf("loading running stack config from %s: %w", st.StackFile, parseErr)
	}

	return current, nil
}

// printLinkActions renders the declared client link section after the stack
// diff. These reconcile on `gridctl apply` (not on plan -y, which deploys
// directly), so the section is informational.
func printLinkActions(w io.Writer, links []linkAction) {
	if len(links) == 0 {
		return
	}
	fmt.Fprintf(w, "\nDeclared client links (reconciled on apply):\n")
	for _, l := range links {
		var line string
		switch l.Action {
		case "link":
			line = fmt.Sprintf("+ %s -> %s (link)", l.Slug, l.Name)
		case "already-linked":
			line = fmt.Sprintf("= %s -> %s (already linked)", l.Slug, l.Name)
		default:
			line = fmt.Sprintf("! %s (skipped: not detected)", l.Slug)
		}
		fmt.Fprintf(w, "  %s\n", line)
	}
}

// printPlanDiff renders the human plan view. Symbols mirror the Terraform
// convention: + add (green), - destroy (red), ~ update (amber). Colors
// follow the color contract, so piped output stays plain.
func printPlanDiff(w io.Writer, diff *config.PlanDiff) {
	if !diff.HasChanges {
		fmt.Fprintln(w, "No changes. Stack is up to date.")
		return
	}

	color := output.ColorEnabled(w)
	header := fmt.Sprintf("Plan: %s", diff.Summary)
	if color {
		header = lipgloss.NewStyle().Foreground(output.ColorAmber).Bold(true).Render(header)
	}
	fmt.Fprintf(w, "%s\n\n", header)

	muted := lipgloss.NewStyle().Foreground(output.ColorMuted)
	for _, item := range diff.Items {
		var symbol, label string
		var symbolColor lipgloss.Color
		switch item.Action {
		case config.DiffAdd:
			symbol, label, symbolColor = "+", "add", output.ColorGreen
		case config.DiffRemove:
			symbol, label, symbolColor = "-", "destroy", output.ColorRed
		case config.DiffChange:
			symbol, label, symbolColor = "~", "update", output.ColorAmber
		}

		line := fmt.Sprintf("%s %s %q (%s)", symbol, item.Kind, item.Name, label)
		if color {
			line = lipgloss.NewStyle().Foreground(symbolColor).Render(line)
		}
		fmt.Fprintf(w, "  %s\n", line)
		for _, detail := range item.Details {
			if color {
				detail = muted.Render(detail)
			}
			fmt.Fprintf(w, "      %s\n", detail)
		}
	}
}
