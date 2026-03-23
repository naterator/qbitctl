package main

import (
	"errors"
	"fmt"
	"os"
	"strings"

	qbt "github.com/naterator/qbitctl/pkg/client"

	"github.com/spf13/cobra"
)

var qbitctlVersion = qbt.Version

const (
	exitOK         = qbt.ExitOK
	exitLoginFail  = qbt.ExitLoginFail
	exitFetchFail  = qbt.ExitFetchFail
	exitSetFail    = qbt.ExitSetFail
	exitBadArgs    = qbt.ExitBadArgs
	exitFile       = qbt.ExitFile
	exitActionFail = qbt.ExitActionFail
)

type (
	App          = qbt.App
	CLIOptions   = qbt.Options
	Credentials  = qbt.Credentials
	TorrentInfo  = qbt.TorrentInfo
	TrackerEntry = qbt.TrackerEntry
)

func errf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "[ERROR] "+format+"\n", args...)
}

type exitError struct {
	code int
}

func (e exitError) Error() string {
	return "exit"
}

var (
	getFieldNames = append(append([]string{}, qbt.GetFieldNames...), "tracker-list")
	setFieldNames = qbt.SetFieldNames
)

func wrapFields(fields []string, indent string, maxWidth int) string {
	var lines []string
	line := indent
	for i, f := range fields {
		entry := f
		if i < len(fields)-1 {
			entry += ", "
		}
		if len(line)+len(entry) > maxWidth && line != indent {
			lines = append(lines, strings.TrimRight(line, " "))
			line = indent
		}
		line += entry
	}
	if line != indent {
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

func result(err error) error {
	if err == nil {
		return nil
	}
	var coded *qbt.CodedError
	if errors.As(err, &coded) {
		errf("%s", coded.Message)
		return exitError{code: coded.Code}
	}
	errf("%v", err)
	return exitError{code: exitBadArgs}
}

func newAuthenticatedApp(opts *CLIOptions) (*App, error) {
	app, err := qbt.NewClient(opts)
	if err != nil {
		return nil, result(err)
	}
	return app, nil
}

func applyPositionalHash(opts *CLIOptions, positionalHash string) {
	if positionalHash != "" {
		opts.Hash = positionalHash
	}
}

func executeConfigWrite(opts CLIOptions, outputPath string) error {
	return result(qbt.ExecuteConfigWrite(opts, outputPath, os.Stderr))
}

func makeHiddenInput(in *os.File) qbt.HiddenInputFunc {
	return func(fn func() (string, error)) (string, error) {
		restore, err := disableTerminalEcho(in)
		if err == nil {
			defer func() {
				_ = restore()
				fmt.Println()
			}()
		}
		return fn()
	}
}

func newRootCmd() *cobra.Command {
	var opts CLIOptions

	rootCmd := &cobra.Command{
		Use:           "qbitctl",
		Short:         "qBittorrent Web API CLI",
		Version:       qbitctlVersion,
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
	rootCmd.CompletionOptions.DisableDefaultCmd = true
	rootCmd.SetVersionTemplate("qbitctl {{.Version}}\n")

	rootCmd.AddGroup(
		&cobra.Group{ID: "view", Title: "View:"},
		&cobra.Group{ID: "change", Title: "Change:"},
		&cobra.Group{ID: "control", Title: "Control:"},
		&cobra.Group{ID: "setup", Title: "Setup:"},
	)

	rootCmd.PersistentFlags().StringVarP(&opts.ConfigPath, "config", "c", "", "Alternate config file path")
	rootCmd.PersistentFlags().StringVarP(&opts.URL, "url", "u", "", "qBittorrent WebUI URL")
	rootCmd.PersistentFlags().StringVarP(&opts.User, "user", "U", "", "qBittorrent username")
	rootCmd.PersistentFlags().StringVarP(&opts.Pass, "pass", "p", "", "qBittorrent password")

	rootCmd.AddCommand(
		newConfigCmd(&opts),
		newCompletionCmd(rootCmd),
		newVersionCmd(),
		newSelfupdateCmd(),
		newInfoCmd(&opts),
		newAddCmd(&opts),
		newListCmd(&opts),
		newShowCmd(&opts),
		newGetCmd(&opts),
		newSetCmd(&opts),
		newHashCmd(&opts, "start [hash]", "Start a torrent", []string{"st"}, (*App).StartTorrent),
		newHashCmd(&opts, "pause [hash]", "Pause a torrent", []string{"p", "pa"}, (*App).PauseTorrent),
		newHashCmd(&opts, "force-start [hash]", "Force start a torrent", []string{"f", "fo", "fs"}, (*App).ForceStartTorrent),
		newMoveCmd(&opts),
		newRemoveCmd(&opts, false),
		newRemoveCmd(&opts, true),
	)

	rootCmd.InitDefaultHelpCmd()
	for _, cmd := range rootCmd.Commands() {
		if cmd.Name() == "help" {
			cmd.Aliases = []string{"h", "he"}
			break
		}
	}

	return rootCmd
}

func execute() int {
	err := newRootCmd().Execute()
	if err == nil {
		return exitOK
	}

	var codeErr exitError
	if errors.As(err, &codeErr) {
		return codeErr.code
	}

	errf("%v", err)
	return exitBadArgs
}

func main() {
	os.Exit(execute())
}
