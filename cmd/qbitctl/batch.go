package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/spf13/cobra"
)

// batchAppKey carries the one authenticated App through each child command.
// The key is private to this package so a command line cannot forge it.
type batchAppKey struct{}

func withBatchApp(ctx context.Context, app *App) context.Context {
	return context.WithValue(ctx, batchAppKey{}, app)
}

func batchAppFromContext(ctx context.Context) (*App, bool) {
	if ctx == nil {
		return nil, false
	}
	app, ok := ctx.Value(batchAppKey{}).(*App)
	return app, ok && app != nil
}

func newBatchCmd(opts *CLIOptions) *cobra.Command {
	var continueOnError bool
	cmd := &cobra.Command{
		Use:     "batch [file]",
		Aliases: []string{"bat"},
		Short:   "Run multiple commands with one authenticated session",
		Long: "Read one qbitctl subcommand per line from FILE or standard input. " +
			"Use single or double quotes to group arguments and # to start a comment. " +
			"The batch command supplies the connection and credentials for every line.",
		GroupID: "control",
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var input io.Reader = cmd.InOrStdin()
			if len(args) == 1 && args[0] != "-" {
				file, err := os.Open(args[0])
				if err != nil {
					return result(cmd, codedErr(exitFile, fmt.Sprintf("open batch file %q: %v", args[0], err)))
				}
				defer file.Close()
				input = file
			}
			return authenticatedRun(opts, func(app *App, _ []string) error {
				return runBatch(cmd, app, input, continueOnError)
			})(cmd, args)
		},
	}
	cmd.Flags().BoolVar(&continueOnError, "continue-on-error", false, "Continue after a command fails and return the first failure")
	return cmd
}

func runBatch(cmd *cobra.Command, app *App, input io.Reader, continueOnError bool) error {
	scanner := bufio.NewScanner(input)
	const maxBatchLine = 64 * 1024
	scanner.Buffer(make([]byte, 4096), maxBatchLine)
	var firstErr error
	for lineNumber := 1; scanner.Scan(); lineNumber++ {
		if err := app.Ctx.Err(); err != nil {
			return err
		}
		if err := executeBatchLine(cmd, app, scanner.Text()); err != nil {
			wrapped := batchCommandError(lineNumber, err)
			if !continueOnError {
				return wrapped
			}
			if firstErr == nil {
				firstErr = wrapped
			} else {
				cmd.PrintErrf("[ERROR] %v\n", wrapped)
			}
		}
	}
	if err := app.Ctx.Err(); err != nil {
		return err
	}
	if err := scanner.Err(); err != nil && firstErr == nil {
		return fmt.Errorf("read batch input: %w", err)
	}
	return firstErr
}

func executeBatchLine(cmd *cobra.Command, app *App, line string) error {
	args, err := parseBatchLine(line)
	if err != nil || len(args) == 0 {
		return err
	}
	if strings.TrimSuffix(filepath.Base(args[0]), ".exe") == "qbitctl" {
		args = args[1:]
		if len(args) == 0 {
			return fmt.Errorf("a qbitctl subcommand is required")
		}
	}
	child := newRootCmd()
	child.SetArgs(args)
	child.SetOut(cmd.OutOrStdout())
	child.SetErr(cmd.ErrOrStderr())
	child.SetIn(strings.NewReader(""))
	child.SetContext(withBatchApp(app.Ctx, app))
	connection := app.ConnectionOptions()
	flags := child.PersistentFlags()
	for name, value := range map[string]string{
		"config": connection.ConfigPath, "profile": connection.Profile,
		"url": connection.URL, "user": connection.User,
		"ca-file": connection.CAFile, "units": string(connection.DataUnits),
	} {
		if value != "" {
			if err := flags.Lookup(name).Value.Set(value); err != nil {
				return fmt.Errorf("set batch %s: %w", name, err)
			}
		}
	}
	return child.Execute()
}

func validateBatchCommand(cmd *cobra.Command) error {
	group := cmd
	root := cmd.Root()
	for group.HasParent() && group.Parent() != root {
		group = group.Parent()
	}
	switch group.Name() {
	case "add", "categories", "delete", "files", "force", "get", "info", "list", "move", "pause",
		"peers", "queue", "reannounce", "recheck", "remove", "rename", "search", "set",
		"show", "start", "tags", "trackers", "transfer", "version":
	default:
		return fmt.Errorf("command %q is not supported inside batch", cmd.Name())
	}
	for _, name := range []string{"config", "profile", "url", "user", "pass", "pass-stdin", "ca-file", "http-timeout"} {
		if flag := cmd.Flag(name); flag != nil && flag.Changed {
			return fmt.Errorf("--%s must be supplied to the parent batch command", name)
		}
	}
	return nil
}

func batchCommandError(line int, err error) error {
	var exit exitError
	if errors.As(err, &exit) {
		return codedErr(exit.code, fmt.Sprintf("batch line %d failed", line))
	}
	return fmt.Errorf("batch line %d: %w", line, err)
}

// parseBatchLine implements the small, predictable subset of shell syntax
// useful for command arguments without ever invoking a shell. Quotes may group
// spaces, backslash escapes the next rune, and # starts a comment at a token
// boundary. A command line is bounded by the scanner to keep batch input safe.
func parseBatchLine(line string) ([]string, error) {
	var args []string
	var token strings.Builder
	var quote rune
	escaped := false
	tokenStarted := false
	for _, r := range line {
		if escaped {
			token.WriteRune(r)
			tokenStarted = true
			escaped = false
			continue
		}
		if quote != 0 {
			if r == quote {
				quote = 0
				continue
			}
			if quote == '"' && r == '\\' {
				escaped = true
				continue
			}
			token.WriteRune(r)
			tokenStarted = true
			continue
		}
		switch {
		case r == '\\':
			escaped = true
			tokenStarted = true
		case r == '\'' || r == '"':
			quote = r
			tokenStarted = true
		case r == '#' && !tokenStarted:
			return args, nil
		case unicode.IsSpace(r):
			if tokenStarted {
				args = append(args, token.String())
				token.Reset()
				tokenStarted = false
			}
		default:
			token.WriteRune(r)
			tokenStarted = true
		}
	}
	if escaped {
		return nil, fmt.Errorf("trailing escape")
	}
	if quote != 0 {
		return nil, fmt.Errorf("unterminated quote")
	}
	if tokenStarted {
		args = append(args, token.String())
	}
	return args, nil
}
