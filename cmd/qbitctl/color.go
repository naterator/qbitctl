package main

import (
	"bytes"
	"image/color"
	"io"
	"os"
	"strings"

	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/term"
	"github.com/naterator/qbitctl/internal/configfile"
	"github.com/naterator/qbitctl/internal/terminaltheme"
	"github.com/spf13/cobra"
)

type cliStyle struct {
	palette terminaltheme.Palette
	profile colorprofile.Profile
}

// The command's own stream controls color. FORCE_COLOR applies to help and
// palette previews, while ordinary results require a terminal.
func newCLIStyle(out io.Writer, explicitPath string, allowForce bool) cliStyle {
	if _, disabled := os.LookupEnv("NO_COLOR"); disabled ||
		strings.EqualFold(strings.TrimSpace(os.Getenv("TERM")), "dumb") {
		return cliStyle{}
	}
	env := os.Environ()
	profile := colorprofile.NoTTY
	if file, ok := out.(term.File); ok && term.IsTerminal(file.Fd()) {
		profile = colorprofile.Detect(out, env)
	}
	if force := strings.TrimSpace(os.Getenv("FORCE_COLOR")); allowForce && force != "" && force != "0" {
		profile = max(profile, colorprofile.Env(env), colorprofile.ANSI256)
	}
	if profile < colorprofile.ANSI {
		return cliStyle{}
	}
	return cliStyle{palette: loadCLIPalette(explicitPath), profile: profile}
}

func loadCLIPalette(explicitPath string) terminaltheme.Palette {
	id := terminaltheme.Default
	if path, err := configfile.ResolvePath(explicitPath); err == nil {
		id, _ = terminaltheme.Load(path)
	}
	return terminaltheme.Lookup(id)
}

func (s cliStyle) paint(text string, foreground color.Color, bold bool) string {
	if s.profile < colorprofile.ANSI || text == "" {
		return text
	}
	style := ansi.Style{}.ForegroundColor(s.profile.Convert(foreground))
	if bold {
		style = style.Bold()
	}
	return style.Styled(text)
}

func (s cliStyle) heading(text string) string     { return s.paintUse(headingUse, text) }
func (s cliStyle) command(text string) string     { return s.paintUse(commandUse, text) }
func (s cliStyle) description(text string) string { return s.paintUse(descriptionUse, text) }
func (s cliStyle) warning(text string) string     { return s.paintUse(warningUse, text) }
func (s cliStyle) diagnostic(text string) string  { return s.paintUse(errorUse, text) }

func commandConfigPath(cmd *cobra.Command) string {
	path, _ := cmd.Root().PersistentFlags().GetString("config")
	return path
}

// styledCLIOutput carries styles for human renderers. Regular writes, including
// structured data, pass through unchanged.
type styledCLIOutput struct {
	term.File
	style cliStyle
}

func cliOutput(out io.Writer, path string) io.Writer {
	style := newCLIStyle(out, path, false)
	if style.profile < colorprofile.ANSI {
		return out
	}
	return &styledCLIOutput{File: out.(term.File), style: style}
}

func (w *styledCLIOutput) StyleLabel(label string) string { return w.style.command(label) }
func (w *styledCLIOutput) StyleRule(rule string) string {
	return w.style.paintUse(separatorUse, rule)
}

func tableHeading(w io.Writer, heading string) string {
	if out, ok := w.(*styledCLIOutput); ok {
		return out.style.heading(heading)
	}
	return heading
}

// tableOutput adds color after tabwriter has calculated column widths.
func tableOutput(w io.Writer) io.Writer {
	if out, ok := w.(*styledCLIOutput); ok {
		return &tableHeadingWriter{out: out}
	}
	return w
}

type tableHeadingWriter struct {
	out          *styledCLIOutput
	heading      bytes.Buffer
	wroteHeading bool
}

func (w *tableHeadingWriter) Write(p []byte) (int, error) {
	if w.wroteHeading {
		return w.out.Write(p)
	}
	i := bytes.IndexByte(p, '\n')
	if i < 0 {
		return w.heading.Write(p)
	}
	w.heading.Write(p[:i])
	if _, err := io.WriteString(w.out, w.out.style.heading(w.heading.String())); err != nil {
		return 0, err
	}
	w.heading = bytes.Buffer{}
	w.wroteHeading = true
	n, err := w.out.Write(p[i:])
	return i + n, err
}
