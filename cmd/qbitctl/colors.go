package main

import (
	"errors"
	"fmt"
	"image/color"
	"io"
	"strings"

	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
	"github.com/naterator/qbitctl/internal/terminaltheme"
	"github.com/spf13/cobra"
)

// The order also assigns stable letters to the preview rows.
const (
	canvasRole = iota
	panelRole
	fieldRole
	textRole
	mutedRole
	accentRole
	keyRole
	ruleRole
	selectedForegroundRole
	selectedBackgroundRole
	errorRole
	warningRole
	scrimBackgroundRole
	scrimForegroundRole
)

type paletteColor struct {
	name  string
	value func(terminaltheme.Palette) color.Color
}

var paletteColors = [...]paletteColor{
	canvasRole:             {"Canvas", func(p terminaltheme.Palette) color.Color { return p.Canvas }},
	panelRole:              {"Panel", func(p terminaltheme.Palette) color.Color { return p.Panel }},
	fieldRole:              {"Field", func(p terminaltheme.Palette) color.Color { return p.Field }},
	textRole:               {"Text", func(p terminaltheme.Palette) color.Color { return p.Text }},
	mutedRole:              {"Muted", func(p terminaltheme.Palette) color.Color { return p.Muted }},
	accentRole:             {"Accent", func(p terminaltheme.Palette) color.Color { return p.Accent }},
	keyRole:                {"Key", func(p terminaltheme.Palette) color.Color { return p.Key }},
	ruleRole:               {"Rule", func(p terminaltheme.Palette) color.Color { return p.Rule }},
	selectedForegroundRole: {"Selected foreground", func(p terminaltheme.Palette) color.Color { return p.SelectedForeground }},
	selectedBackgroundRole: {"Selected background", func(p terminaltheme.Palette) color.Color { return p.SelectedBackground }},
	errorRole:              {"Error", func(p terminaltheme.Palette) color.Color { return p.Error }},
	warningRole:            {"Warning", func(p terminaltheme.Palette) color.Color { return p.Warning }},
	scrimBackgroundRole:    {"Scrim background", func(p terminaltheme.Palette) color.Color { return p.ScrimBackground }},
	scrimForegroundRole:    {"Scrim foreground", func(p terminaltheme.Palette) color.Color { return p.ScrimForeground }},
}

const (
	headingUse = iota
	commandUse
	descriptionUse
	separatorUse
	errorUse
	warningUse
)

type cliColorUse struct {
	role   int
	sample string
	bold   bool
}

// The renderers and the preview share this mapping.
var cliColorUses = [...]cliColorUse{
	headingUse:     {role: textRole, sample: "Headings and table headers", bold: true},
	commandUse:     {role: accentRole, sample: "Commands and field labels"},
	descriptionUse: {role: mutedRole, sample: "Help descriptions"},
	separatorUse:   {role: ruleRole, sample: "Field separators ....|"},
	errorUse:       {role: errorRole, sample: "[ERROR]", bold: true},
	warningUse:     {role: warningRole, sample: "Delete confirmation warning"},
}

func (s cliStyle) paintUse(index int, text string) string {
	use := cliColorUses[index]
	return s.paint(text, paletteColors[use.role].value(s.palette), use.bold)
}

var errUnknownDebug = errors.New(`unknown command "debug" for "qbitctl"`)

func newDebugCmd() *cobra.Command {
	debug := &cobra.Command{
		Use:    "debug",
		Short:  "Developer diagnostics",
		Hidden: true,
		Args:   func(*cobra.Command, []string) error { return errUnknownDebug },
		RunE:   func(*cobra.Command, []string) error { return errUnknownDebug },
	}
	mapping := &cobra.Command{
		Use:    "color-mapping",
		Short:  "Preview the active terminal palette",
		Hidden: true,
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) != 0 {
				return errUnknownDebug
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			out, path := cmd.OutOrStdout(), commandConfigPath(cmd)
			style := newCLIStyle(out, path, true)
			if style.palette.ID == "" {
				style.palette = loadCLIPalette(path)
			}
			return renderColors(out, style)
		},
	}
	for _, cmd := range []*cobra.Command{debug, mapping} {
		cmd.Flags().VarP(unavailableDebugHelp{}, "help", "h", "")
		flag := cmd.Flags().Lookup("help")
		flag.NoOptDefVal, flag.Hidden = "true", true
		cmd.SetFlagErrorFunc(func(*cobra.Command, error) error { return errUnknownDebug })
	}
	debug.AddCommand(mapping)
	return debug
}

type unavailableDebugHelp struct{}

func (unavailableDebugHelp) String() string   { return "false" }
func (unavailableDebugHelp) Type() string     { return "bool" }
func (unavailableDebugHelp) Set(string) error { return errUnknownDebug }

func renderColors(out io.Writer, style cliStyle) error {
	var preview strings.Builder
	fmt.Fprintf(&preview, "%s\n\n   %-20s %-7s  %s\n", style.palette.Label, "TUI role", "Swatch", "CLI sample")
	for index, role := range paletteColors {
		var samples []string
		for useIndex, use := range cliColorUses {
			if use.role == index {
				samples = append(samples, style.paintUse(useIndex, use.sample))
			}
		}
		if len(samples) == 0 {
			samples = []string{"-"}
		}
		fmt.Fprintf(&preview, "%c  %-20s %s  %s\n",
			'A'+index, role.name, colorSwatch(role.value(style.palette), style.profile, 7), strings.Join(samples, "  "))
	}
	_, err := io.WriteString(out, preview.String())
	return err
}

func paletteSwatches(theme terminaltheme.Palette, profile colorprofile.Profile) string {
	swatches := make([]string, 0, len(paletteColors))
	for _, role := range paletteColors {
		swatches = append(swatches, colorSwatch(role.value(theme), profile, 1))
	}
	separator := ""
	if profile < colorprofile.ANSI {
		separator = " "
	}
	return strings.Join(swatches, separator)
}

func colorSwatch(value color.Color, profile colorprofile.Profile, width int) string {
	if profile >= colorprofile.ANSI {
		return ansi.Style{}.BackgroundColor(profile.Convert(value)).Styled(strings.Repeat(" ", width))
	}
	r, g, b, _ := value.RGBA()
	return fmt.Sprintf("#%02X%02X%02X", r>>8, g>>8, b>>8)
}
