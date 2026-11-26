package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"syscall"

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

func printCLIError(cmd *cobra.Command, err error) {
	prefix := "[ERROR]"
	if _, inBatch := batchAppFromContext(cmd.Context()); !inBatch && cmd.Name() != "batch" {
		style := newCLIStyle(cmd.ErrOrStderr(), commandConfigPath(cmd), false)
		prefix = style.diagnostic(prefix)
	}
	cmd.PrintErrf("%s %v\n", prefix, err)
}

func result(cmd *cobra.Command, err error) error {
	if err == nil {
		return nil
	}
	printCLIError(cmd, err)
	var coded *qbt.CodedError
	if errors.As(err, &coded) {
		return exitError{code: coded.Code}
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return exitError{code: exitFetchFail}
	}
	return exitError{code: exitBadArgs}
}

func authenticatedRun(opts *CLIOptions, run func(*App, []string) error) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		if app, ok := batchAppFromContext(cmd.Context()); ok {
			// A batch owns one authenticated App. Commands still get their own
			// Cobra output streams, while the underlying session and version pair
			// remain in memory for the whole batch.
			previousStdout, previousStderr := app.Stdout, app.Stderr
			previousUnits := app.DataUnits
			app.Stdout, app.Stderr = cmd.OutOrStdout(), cmd.ErrOrStderr()
			if opts.DataUnits != "" {
				app.DataUnits = opts.DataUnits
			}
			defer func() {
				app.Stdout, app.Stderr = previousStdout, previousStderr
				app.DataUnits = previousUnits
			}()
			return result(cmd, run(app, args))
		}
		authOpts := *opts
		authOpts.UseCompatibilityCache = true
		if interactiveWorkspace(cmd) {
			if setup, err := qbt.ReadSetup(authOpts); err == nil && !setup.Ready() {
				finished, err := firstRunSetup(cmd, &authOpts, setup.Path)
				if finished || err != nil {
					return err
				}
			}
		}
		ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
		defer stop()

		app, err := qbt.NewClientWithOutput(ctx, &authOpts, cliOutput(cmd.OutOrStdout(), authOpts.ConfigPath), cmd.ErrOrStderr())
		if err != nil {
			return result(cmd, err)
		}
		defer app.Logout()
		return result(cmd, run(app, args))
	}
}

func makeHiddenInput(in io.Reader, out io.Writer) qbt.HiddenInputFunc {
	return func(fn func() (string, error)) (string, error) {
		file, ok := in.(*os.File)
		if !ok {
			return fn()
		}
		restore, err := disableTerminalEcho(file)
		if err == nil {
			defer func() {
				_ = restore()
				fmt.Fprintln(out)
			}()
		}
		return fn()
	}
}

func newRootCmd() *cobra.Command {
	var opts CLIOptions
	var passStdin bool
	unitsValue := ""

	rootCmd := &cobra.Command{
		Use:           "qbitctl",
		Short:         "qBittorrent Web API CLI",
		Version:       qbitctlVersion,
		SilenceErrors: true,
		SilenceUsage:  true,
		Args:          cobra.NoArgs,
		Annotations:   map[string]string{stdinModeAnnotation: stdinInteractive},
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			units, err := parseDataUnits(unitsValue)
			if err != nil {
				return result(cmd, err)
			}
			opts.DataUnits = units
			if _, ok := batchAppFromContext(cmd.Context()); ok {
				return result(cmd, validateBatchCommand(cmd))
			}
			if cmd.Name() == "batch" && cmd.Flags().Changed("pass-stdin") && (len(args) == 0 || args[0] == "-") {
				return result(cmd, codedErr(exitBadArgs, "--pass-stdin requires a batch file because both inputs use standard input"))
			}
			return result(cmd, resolvePassword(cmd, &opts, passStdin))
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runDefault(cmd, opts)
		},
	}
	rootCmd.CompletionOptions.DisableDefaultCmd = true
	rootCmd.SetVersionTemplate("qbitctl {{.Version}}\n")
	rootCmd.SetFlagErrorFunc(func(cmd *cobra.Command, err error) error {
		return codedErr(exitBadArgs, err.Error())
	})

	rootCmd.AddGroup(
		&cobra.Group{ID: "view", Title: "View:"},
		&cobra.Group{ID: "change", Title: "Change:"},
		&cobra.Group{ID: "control", Title: "Control:"},
		&cobra.Group{ID: "setup", Title: "Setup:"},
	)

	rootCmd.PersistentFlags().StringVarP(&opts.ConfigPath, "config", "c", "", "Alternate config file path")
	rootCmd.PersistentFlags().StringVar(&opts.Profile, "profile", "", "Saved server profile for this invocation (session-only)")
	rootCmd.PersistentFlags().StringVarP(&opts.URL, "url", "u", "", "qBittorrent WebUI URL")
	rootCmd.PersistentFlags().StringVarP(&opts.User, "user", "U", "", "qBittorrent username")
	rootCmd.PersistentFlags().StringVarP(&opts.Pass, "pass", "p", "", "qBittorrent password (visible in process arguments; prefer --pass-stdin or QBITCTL_PASSWORD)")
	rootCmd.PersistentFlags().BoolVar(&passStdin, "pass-stdin", false, "Read the qBittorrent password from standard input")
	rootCmd.PersistentFlags().StringVar(&opts.CAFile, "ca-file", "", "PEM CA bundle for the qBittorrent WebUI connection")
	rootCmd.PersistentFlags().StringVar(&unitsValue, "units", "", "Data-size output units: human or bytes; empty uses config (human fallback)")
	rootCmd.PersistentFlags().DurationVar(&opts.HTTPTimeout, "http-timeout", 0, "Maximum time for each qBittorrent request (default: 15s; full lists: 60s; uploads: 2m)")
	rootCmd.MarkFlagsMutuallyExclusive("pass", "pass-stdin")
	_ = rootCmd.RegisterFlagCompletionFunc("profile", completeProfileArgs)

	rootCmd.AddCommand(
		newConfigCmd(&opts),
		newProfileCmd(&opts),
		newTUIDefaultCmd(&opts),
		newCompletionCmd(rootCmd),
		newVersionCmd(),
		newUpdateCmd(),
		newBatchCmd(&opts),
		newInfoCmd(&opts),
		newTransferCmd(&opts),
		newTUICmd(&opts),
		newSearchCmd(&opts),
		newFilesCmd(&opts),
		newPeersCmd(&opts),
		newTrackersCmd(&opts),
		newAddCmd(&opts),
		newCategoriesCmd(&opts),
		newTagsCmd(&opts),
		newListCmd(&opts),
		newShowCmd(&opts),
		newGetCmd(&opts),
		newSetCmd(&opts),
		newHashCmd(&opts, "start <hash...>", "Start one or more torrents", []string{"st"}, (*App).StartTorrent),
		newHashCmd(&opts, "pause <hash...>", "Pause one or more torrents", []string{"p", "pa"}, (*App).PauseTorrent),
		newHashCmd(&opts, "force-start <hash...>", "Force start one or more torrents", []string{"f", "fo", "fs"}, (*App).ForceStartTorrent),
		newRecheckCmd(&opts),
		newReannounceCmd(&opts),
		newQueueCmd(&opts),
		newRenameCmd(&opts),
		newMoveCmd(&opts),
		newRemoveCmd(&opts, false),
		newRemoveCmd(&opts, true),
	)

	installHelp(rootCmd)
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
	cmd := newRootCmd()
	err := cmd.Execute()
	if err == nil {
		return exitOK
	}

	var codeErr exitError
	if errors.As(err, &codeErr) {
		return codeErr.code
	}

	printCLIError(cmd, err)
	return exitBadArgs
}

func main() {
	removePreviousExecutable(runtime.GOOS, os.Executable, os.Remove)
	os.Exit(execute())
}
