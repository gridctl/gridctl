package main

import (
	"os"
	"strings"

	"github.com/gridctl/gridctl/pkg/output"

	"github.com/spf13/cobra"
)

// ANSI color codes based on Obsidian Observatory design system
const (
	colorAmber  = "\033[38;2;245;158;11m"  // #f59e0b - Primary (section headers)
	colorTeal   = "\033[38;2;13;148;136m"  // #0d9488 - Secondary (commands/flags)
	colorPurple = "\033[38;2;139;92;246m"  // #8b5cf6 - Tertiary (arguments)
	colorWhite  = "\033[38;2;250;250;249m" // #fafaf9 - Text primary
	colorMuted  = "\033[38;2;120;113;108m" // #78716c - Text muted
	colorReset  = "\033[0m"
)

// helpColorEnabled reports whether help output should carry ANSI colors.
// Checked lazily at render time because --help bypasses PersistentPreRun,
// so the --no-color flag must be consulted directly.
func helpColorEnabled() bool {
	return !noColorFlag && output.ColorEnabled(os.Stdout)
}

// colorize applies color to text when color output is enabled.
func colorize(color, text string) string {
	if !helpColorEnabled() {
		return text
	}
	return color + text + colorReset
}

// Custom help template matching Containerlab style with Obsidian Observatory
// colors. Sections render via the header func so colors respect the color
// contract (TTY, NO_COLOR, TERM=dumb, --no-color). Commands render under
// cobra groups on the root; ungrouped subcommands keep a flat list. The
// trailing subcommand footer only renders for commands that have them.
var helpTemplate = `{{with .Long}}{{. | trimTrailingWhitespaces}}

{{end}}{{if not .HasParent}}{{header "QUICK START"}}
  gridctl apply stack.yaml    Deploy a stack of MCP servers
  gridctl link                Connect your LLM client
  gridctl status              See what is running

{{end}}{{header "USAGE"}}
  {{.UseLine | colorizeUsage}}
{{if and .HasAvailableSubCommands .Groups}}{{$root := .}}{{range $grp := .Groups}}
{{header $grp.Title}}
{{range $root.Commands}}{{if and (eq .GroupID $grp.ID) .IsAvailableCommand}}  {{colorizeCmd .Name}} {{.Short}}
{{end}}{{end}}{{end}}{{if not .AllChildCommandsHaveGroup}}
{{header "OTHER"}}
{{range .Commands}}{{if and (eq .GroupID "") .IsAvailableCommand}}  {{colorizeCmd .Name}} {{.Short}}
{{end}}{{end}}{{end}}{{else if .HasAvailableSubCommands}}
{{header "COMMANDS"}}
{{range .Commands}}{{if .IsAvailableCommand}}  {{colorizeCmd .Name}} {{.Short}}
{{end}}{{end}}{{end}}{{if .HasAvailableLocalFlags}}
{{header "FLAGS"}}
{{.LocalFlags.FlagUsages | colorizeFlags}}{{end}}{{if .HasAvailableInheritedFlags}}
{{header "GLOBAL FLAGS"}}
{{.InheritedFlags.FlagUsages | colorizeFlags}}{{end}}{{if .HasExample}}
{{header "EXAMPLES"}}
{{.Example}}
{{end}}{{if .HasAvailableSubCommands}}
Use "{{.CommandPath}} [command] --help" for more information about a command.
{{end}}`

// header renders a section header in amber when color is enabled.
func header(title string) string {
	return colorize(colorAmber, title)
}

// colorizeUsage colors the usage line
func colorizeUsage(usage string) string {
	if !helpColorEnabled() {
		return usage
	}
	// Color the command name in teal, arguments in purple, flags in muted
	parts := strings.Fields(usage)
	if len(parts) == 0 {
		return usage
	}

	var result []string
	for i, part := range parts {
		if i == 0 {
			// Command name in teal
			result = append(result, colorize(colorTeal, part))
		} else if strings.HasPrefix(part, "[flags]") || strings.HasPrefix(part, "[--") {
			// Flags in muted gray (like Containerlab)
			result = append(result, colorize(colorMuted, part))
		} else if strings.HasPrefix(part, "<") || strings.HasPrefix(part, "[") {
			// Required/optional arguments in purple
			result = append(result, colorize(colorPurple, part))
		} else {
			// Subcommands in teal
			result = append(result, colorize(colorTeal, part))
		}
	}
	return strings.Join(result, " ")
}

// colorizeCmd colors command names and pads to consistent width
func colorizeCmd(name string) string {
	// Pad the name first, then colorize (so padding isn't affected by escape codes)
	padded := name
	if len(name) < 14 {
		padded = name + strings.Repeat(" ", 14-len(name))
	}
	return colorize(colorTeal, padded)
}

// colorizeFlags colors flag definitions
func colorizeFlags(flags string) string {
	if !helpColorEnabled() {
		return flags
	}
	lines := strings.Split(flags, "\n")
	var result []string

	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			result = append(result, line)
			continue
		}

		// Find the flag part (before the description)
		// Flags look like: "  -f, --flag type   description"
		trimmed := strings.TrimLeft(line, " ")
		indent := line[:len(line)-len(trimmed)]

		// Split on multiple spaces to find description
		parts := strings.SplitN(trimmed, "   ", 2)
		if len(parts) == 2 {
			flagPart := parts[0]
			desc := parts[1]

			// Color the flag names (before any type indicator)
			coloredFlag := colorizeFlagPart(flagPart)
			result = append(result, indent+coloredFlag+"   "+desc)
		} else {
			// Just color the whole thing as a flag
			result = append(result, indent+colorizeFlagPart(trimmed))
		}
	}

	return strings.Join(result, "\n")
}

// colorizeFlagPart colors individual flag components
func colorizeFlagPart(flagPart string) string {
	var result strings.Builder
	i := 0

	for i < len(flagPart) {
		switch flagPart[i] {
		case '-':
			result.WriteString(colorTeal)
			// Consume the flag name (until space or comma)
			for i < len(flagPart) && flagPart[i] != ' ' && flagPart[i] != ',' {
				result.WriteByte(flagPart[i])
				i++
			}
			result.WriteString(colorReset)
		case ' ', ',':
			result.WriteByte(flagPart[i])
			i++
		default:
			// Type indicator (e.g., "string", "int")
			result.WriteString(colorPurple)
			for i < len(flagPart) && flagPart[i] != ' ' {
				result.WriteByte(flagPart[i])
				i++
			}
			result.WriteString(colorReset)
		}
	}

	return result.String()
}

// initHelp sets up the custom help template
func initHelp() {
	cobra.AddTemplateFunc("colorizeUsage", colorizeUsage)
	cobra.AddTemplateFunc("colorizeCmd", colorizeCmd)
	cobra.AddTemplateFunc("colorizeFlags", colorizeFlags)
	cobra.AddTemplateFunc("header", header)

	rootCmd.SetHelpTemplate(helpTemplate)
}
