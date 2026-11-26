package main

import (
	_ "embed"
	"encoding/base64"
	"fmt"
	"image/color"
	"io"
	"os"
	"strings"

	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/term"
)

// Rasterized at 2x from assets/logo.svg.
//
//go:embed help_logo.png
var helpLogoPNG []byte

const helpLogoASCII = ` /--------------------------------------------------\
 |   #####                                          |
 |  ##   ##  ##      ++    ##            ##   ##    |
 |  ##   ##  ##            ##            ##   ##    |
 |  ##   ##  #####   ##  ######  ##### ###### ##    |
 |   ######  ##  ##  ##    ##   ##       ##   ##    |
 |       ##  ##  ##  ##    ##   ##       ##   ##    |
 |      ++++ #####  ####    ###  #####    ###  ###  |
 |       ++                                         |
 \--------------------------------------------------/`

const (
	helpLogoColumns = 36
	helpLogoMinRows = 8
)

func writeHelpLogo(out io.Writer, style cliStyle) {
	columns := 0
	if file, ok := out.(term.File); ok && term.IsTerminal(file.Fd()) {
		var rows int
		columns, rows, _ = term.GetSize(file.Fd())
		// Set only the width so the terminal scales the height from the PNG.
		// A fixed rectangle in character cells can distort the logo.
		if style.profile >= colorprofile.ANSI && (columns == 0 || columns >= helpLogoColumns) && (rows == 0 || rows >= helpLogoMinRows) {
			switch helpImageProtocol() {
			case "kitty":
				fmt.Fprintln(out)
				writeKittyHelpLogo(out)
				return
			case "iterm":
				fmt.Fprintf(out, "\n\x1b]1337;File=inline=1;width=%d;preserveAspectRatio=1:%s\a\n\n",
					helpLogoColumns, base64.StdEncoding.EncodeToString(helpLogoPNG))
				return
			}
		}
	}
	logo := helpLogoASCII
	firstLine, _, _ := strings.Cut(logo, "\n")
	if columns > 0 && columns < len(firstLine) {
		logo = "[ qbitctl ]"
	}
	fmt.Fprintf(out, "\n%s\n\n", colorASCIIHelpLogo(logo, style.profile))
}

func colorASCIIHelpLogo(logo string, profile colorprofile.Profile) string {
	if profile < colorprofile.ANSI {
		return logo
	}

	// Match the original fills and outline in assets/logo.svg.
	panel := ansi.Style{}.BackgroundColor(profile.Convert(color.RGBA{R: 0x1d, G: 0x25, B: 0x31, A: 255})).String()
	outline := ansi.Style{}.ForegroundColor(profile.Convert(color.RGBA{R: 0x53, G: 0x64, B: 0x78, A: 255})).String()
	wordmark := ansi.Style{}.ForegroundColor(profile.Convert(color.RGBA{R: 0x9a, G: 0xc8, B: 0xff, A: 255})).String()
	mint := ansi.Style{}.ForegroundColor(profile.Convert(color.RGBA{R: 0x85, G: 0xd7, B: 0xc8, A: 255})).String()

	var out strings.Builder
	for line := range strings.SplitSeq(logo, "\n") {
		drawing := strings.TrimLeft(line, " ")
		out.WriteString(line[:len(line)-len(drawing)])
		out.WriteString(panel)
		foreground := ""
		for _, glyph := range drawing {
			next := foreground
			switch glyph {
			case '/', '\\', '-', '|', '[', ']':
				next = outline
			case '+':
				next = mint
			case ' ':
				// Spaces only need the panel background.
			default:
				next = wordmark
			}
			if next != foreground {
				out.WriteString(next)
				foreground = next
			}
			out.WriteRune(glyph)
		}
		out.WriteString(ansi.ResetStyle)
		out.WriteByte('\n')
	}
	return strings.TrimSuffix(out.String(), "\n")
}

func helpImageProtocol() string {
	if strings.EqualFold(strings.TrimSpace(os.Getenv("QBITCTL_HELP_LOGO")), "ascii") {
		return ""
	}
	terminal := strings.ToLower(os.Getenv("TERM"))
	program := strings.ToLower(os.Getenv("TERM_PROGRAM"))
	// Multiplexers can filter image escapes even when the outer terminal
	// supports them. Keep the logo readable without changing their settings.
	if os.Getenv("TMUX") != "" || os.Getenv("STY") != "" ||
		strings.HasPrefix(terminal, "tmux") || strings.HasPrefix(terminal, "screen") {
		return ""
	}
	switch {
	case terminal == "xterm-kitty", terminal == "xterm-ghostty", program == "ghostty":
		return "kitty"
	case program == "iterm.app", program == "wezterm":
		return "iterm"
	default:
		return ""
	}
}

func writeKittyHelpLogo(out io.Writer) {
	// Kitty limits direct transfers to 4096 base64 bytes per APC. Suppress
	// replies and let the terminal advance the cursor for the scaled image.
	const chunkLength = 4096
	data := base64.StdEncoding.EncodeToString(helpLogoPNG)
	control := fmt.Sprintf("a=T,f=100,q=2,c=%d,", helpLogoColumns)
	for len(data) > chunkLength {
		fmt.Fprintf(out, "\x1b_G%sm=1;%s\x1b\\", control, data[:chunkLength])
		data = data[chunkLength:]
		control = "q=2,"
	}
	fmt.Fprintf(out, "\x1b_G%sm=0;%s\x1b\\\n\n", control, data)
}
