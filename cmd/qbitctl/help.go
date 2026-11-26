package main

import (
	"fmt"
	"io"
	"strings"

	"github.com/charmbracelet/colorprofile"
	"github.com/spf13/cobra"
)

// installHelp installs one help function at the root. Cobra automatically
// inherits a parent's help function, so commands (including future commands)
// continue to use Cobra's own usage hierarchy and flag semantics.
func installHelp(cmd *cobra.Command) {
	if cmd == nil {
		return
	}
	cmd.SetHelpFunc(func(target *cobra.Command, _ []string) {
		renderQbitctlHelp(target)
	})
}

func renderQbitctlHelp(cmd *cobra.Command) {
	if cmd == nil {
		return
	}
	out := cmd.OutOrStdout()
	style := newCLIStyle(out, commandConfigPath(cmd), true)

	// Keep Cobra's description handling (Long over Short, including whitespace
	// trimming) and its UsageString output. This means aliases, grouped
	// commands, examples, local/global flags, and custom Cobra extensions all
	// remain available without maintaining a second command model here.
	description := strings.TrimRight(cmd.Long, " \t\r\n")
	if description == "" {
		description = strings.TrimRight(cmd.Short, " \t\r\n")
	}
	if description != "" {
		writeDescription(out, style, description)
		fmt.Fprintln(out)
	}

	usage := cmd.UsageString()
	fmt.Fprint(out, colorizeUsage(usage, style))
}

func writeDescription(out io.Writer, style cliStyle, text string) {
	if style.profile < colorprofile.ANSI {
		fmt.Fprintln(out, text)
		return
	}
	for _, line := range strings.Split(text, "\n") {
		if line == "" {
			fmt.Fprintln(out)
			continue
		}
		fmt.Fprintln(out, style.description(line))
	}
}

// colorizeUsage only decorates Cobra's already-formatted usage. In plain
// output it returns the exact string Cobra generated, which is important for
// scripts, tests, and users who have customized Cobra's usage template.
func colorizeUsage(usage string, style cliStyle) string {
	if style.profile < colorprofile.ANSI || usage == "" {
		return usage
	}

	var output strings.Builder
	section := ""
	for _, chunk := range strings.SplitAfter(usage, "\n") {
		line := strings.TrimSuffix(chunk, "\n")
		newline := ""
		if strings.HasSuffix(chunk, "\n") {
			newline = "\n"
		}
		output.WriteString(colorizeUsageLine(line, &section, style))
		output.WriteString(newline)
	}
	return output.String()
}

func colorizeUsageLine(line string, section *string, style cliStyle) string {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return line
	}

	// Cobra's section and group headings are the only non-indented lines that
	// end with a colon. Keeping the original text preserves labels such as
	// "View:" and "Additional Commands:".
	if !strings.HasPrefix(line, " ") && strings.HasSuffix(trimmed, ":") {
		*section = usageSection(trimmed)
		return style.heading(line)
	}

	if strings.HasPrefix(trimmed, "Use ") {
		return style.description(line)
	}

	switch *section {
	case "commands", "flags", "global-flags", "help-topics":
		if option, description, ok := splitAlignedLine(line, *section == "commands"); ok {
			return style.command(option) + style.description(description)
		}
		return style.command(line)
	case "usage", "aliases", "examples":
		return style.command(line)
	default:
		// A custom usage template may contain an indented note that is not a
		// command or flag. It is safer to keep that note as a description.
		return style.description(line)
	}
}

func usageSection(title string) string {
	switch strings.ToLower(strings.TrimSuffix(title, ":")) {
	case "usage":
		return "usage"
	case "aliases":
		return "aliases"
	case "examples":
		return "examples"
	case "flags":
		return "flags"
	case "global flags":
		return "global-flags"
	case "additional help topics":
		return "help-topics"
	case "available commands", "additional commands":
		return "commands"
	default:
		// Cobra uses registered group titles (for qbitctl: View:, Change:,
		// Control:, Setup:), which are command lists too.
		return "commands"
	}
}

func splitAlignedLine(line string, commandRow bool) (left, right string, ok bool) {
	trimmed := strings.TrimLeft(line, " \t")
	leading := line[:len(line)-len(trimmed)]
	separator := strings.Index(trimmed, "  ")
	if commandRow {
		// Cobra pads command names and adds one space before their short
		// descriptions. The longest name has no padding.
		separator = strings.IndexAny(trimmed, " \t")
	}
	if separator < 0 {
		return "", "", false
	}
	return leading + trimmed[:separator], trimmed[separator:], true
}
