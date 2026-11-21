package main

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/charmbracelet/x/term"
	"github.com/spf13/cobra"
)

const (
	helpReset = "\x1b[0m"
	helpBold  = "\x1b[1m"
	helpBlue  = "\x1b[38;5;81m"
	helpTeal  = "\x1b[38;5;116m"
	helpSlate = "\x1b[38;5;245m"
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

type helpStyle struct {
	color bool
}

func newHelpStyle(w io.Writer) helpStyle {
	// NO_COLOR always wins. FORCE_COLOR is useful for demos and for terminals
	// wrapped by a logger that does not expose a file descriptor.
	if _, ok := os.LookupEnv("NO_COLOR"); ok {
		return helpStyle{}
	}
	if strings.EqualFold(strings.TrimSpace(os.Getenv("TERM")), "dumb") {
		return helpStyle{}
	}
	if force := strings.TrimSpace(os.Getenv("FORCE_COLOR")); force != "" && force != "0" {
		return helpStyle{color: true}
	}
	if file, ok := w.(*os.File); ok && term.IsTerminal(file.Fd()) {
		return helpStyle{color: true}
	}
	return helpStyle{}
}

func (s helpStyle) heading(value string) string {
	if !s.color {
		return value
	}
	return helpBlue + helpBold + value + helpReset
}

func (s helpStyle) command(value string) string {
	if !s.color {
		return value
	}
	return helpTeal + value + helpReset
}

func (s helpStyle) description(value string) string {
	if !s.color {
		return value
	}
	return helpSlate + value + helpReset
}

func renderQbitctlHelp(cmd *cobra.Command) {
	if cmd == nil {
		return
	}
	out := cmd.OutOrStdout()
	style := newHelpStyle(out)

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

func writeDescription(out io.Writer, style helpStyle, text string) {
	if !style.color {
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
func colorizeUsage(usage string, style helpStyle) string {
	if !style.color || usage == "" {
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

func colorizeUsageLine(line string, section *string, style helpStyle) string {
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
		if option, description, ok := splitAlignedLine(line); ok {
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

func splitAlignedLine(line string) (left, right string, ok bool) {
	trimmed := strings.TrimLeft(line, " \t")
	leading := line[:len(line)-len(trimmed)]
	separator := strings.Index(trimmed, "  ")
	if separator < 0 {
		return "", "", false
	}
	return leading + trimmed[:separator], trimmed[separator:], true
}
