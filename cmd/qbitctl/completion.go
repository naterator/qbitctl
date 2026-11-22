package main

import (
	"context"
	"io"
	"os"
	"strings"
	"time"

	qbt "github.com/naterator/qbitctl/pkg/client"
	"github.com/spf13/cobra"
)

// completionTimeout bounds the complete server interaction, including login,
// compatibility checks, and the torrent list request.
var completionTimeout = 3 * time.Second

func completeStatic(values []string, toComplete string) ([]cobra.Completion, cobra.ShellCompDirective) {
	completions := make([]cobra.Completion, 0, len(values))
	for _, value := range values {
		if strings.HasPrefix(value, toComplete) {
			completions = append(completions, value)
		}
	}
	return completions, cobra.ShellCompDirectiveNoFileComp
}

func completeGetArgs(opts *CLIOptions) cobra.CompletionFunc {
	return func(_ *cobra.Command, args []string, toComplete string) ([]cobra.Completion, cobra.ShellCompDirective) {
		if len(args) == 0 {
			fields := append(append([]string{}, qbt.GetFieldNames...), "tracker-list")
			return completeStatic(fields, toComplete)
		}
		if len(args) > 1 {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		return completeTorrentHashes(opts, nil, toComplete)
	}
}

func completeSetArgs(opts *CLIOptions) cobra.CompletionFunc {
	return func(_ *cobra.Command, args []string, toComplete string) ([]cobra.Completion, cobra.ShellCompDirective) {
		switch len(args) {
		case 0:
			return completeStatic(qbt.SetFieldNames, toComplete)
		case 1:
			return completeSetValue(args[0], toComplete)
		case 2:
			return completeTorrentHashes(opts, args[2:], toComplete)
		default:
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
	}
}

func completeSetValue(field, toComplete string) ([]cobra.Completion, cobra.ShellCompDirective) {
	var values []string
	switch qbt.CanonicalSetField(field) {
	case "seqdl", "autotmm", "superseed":
		values = []string{"true", "false"}
	case "seedtime-limit":
		values = []string{"-1", "-2", "1h", "1d"}
	}
	return completeStatic(values, toComplete)
}

func completeMoveArgs(opts *CLIOptions) cobra.CompletionFunc {
	return func(_ *cobra.Command, args []string, toComplete string) ([]cobra.Completion, cobra.ShellCompDirective) {
		switch len(args) {
		case 0:
			return cobra.NoFileCompletions(nil, nil, toComplete)
		case 1:
			return completeTorrentHashes(opts, args[1:], toComplete)
		default:
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
	}
}

func completeTorrentHashArgs(opts *CLIOptions) cobra.CompletionFunc {
	return func(_ *cobra.Command, args []string, toComplete string) ([]cobra.Completion, cobra.ShellCompDirective) {
		return completeTorrentHashes(opts, args, toComplete)
	}
}

func completeSingleTorrentHashArgs(opts *CLIOptions) cobra.CompletionFunc {
	return func(_ *cobra.Command, args []string, toComplete string) ([]cobra.Completion, cobra.ShellCompDirective) {
		if len(args) > 0 {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		return completeTorrentHashes(opts, nil, toComplete)
	}
}

func completeAddArgs(_ *cobra.Command, args []string, toComplete string) ([]cobra.Completion, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	return []cobra.Completion{"torrent"}, cobra.ShellCompDirectiveFilterFileExt
}

// completeTorrentHashes uses the saved configuration only when it is already
// usable. Completion must never invoke first-run setup or read a password from
// stdin: Cobra's hidden __complete command bypasses PersistentPreRunE.
func completeTorrentHashes(opts *CLIOptions, exclude []string, toComplete string) ([]cobra.Completion, cobra.ShellCompDirective) {
	authOpts := *opts
	if authOpts.Pass == "" {
		if password, ok := os.LookupEnv("QBITCTL_PASSWORD"); ok && password != "" {
			authOpts.Pass = password
		}
	}
	setup, err := qbt.ReadSetup(authOpts)
	if err != nil || !setup.Ready() {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	ctx, cancel := context.WithTimeout(context.Background(), completionTimeout)
	defer cancel()
	app, err := qbt.NewClientWithOutput(ctx, &authOpts, io.Discard, io.Discard)
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp | cobra.ShellCompDirectiveError
	}
	defer app.Logout()

	torrents, _, err := app.FetchAllTorrentsContext(ctx)
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp | cobra.ShellCompDirectiveError
	}

	excluded := make([]string, 0, len(exclude))
	for _, value := range exclude {
		value = strings.ToLower(strings.TrimSpace(value))
		if value != "" {
			excluded = append(excluded, value)
		}
	}
	toComplete = strings.ToLower(toComplete)
	completions := make([]cobra.Completion, 0, min(len(torrents), 200))
	for _, torrent := range torrents {
		hash := strings.ToLower(strings.TrimSpace(torrent.Hash))
		if !isFullTorrentHash(hash) || excludedHash(hash, excluded) {
			continue
		}
		if !strings.HasPrefix(hash, toComplete) && !strings.HasPrefix(strings.ToLower(torrent.Name), toComplete) {
			continue
		}
		completions = append(completions, cobra.CompletionWithDesc(hash, qbt.SanitizeServerText(torrent.Name)))
		if len(completions) == 200 {
			break
		}
	}
	return completions, cobra.ShellCompDirectiveNoFileComp
}

func excludedHash(hash string, excluded []string) bool {
	for _, value := range excluded {
		if strings.HasPrefix(hash, value) {
			return true
		}
	}
	return false
}

func isFullTorrentHash(value string) bool {
	if len(value) != 40 {
		return false
	}
	for _, char := range value {
		if (char < '0' || char > '9') && (char < 'a' || char > 'f') {
			return false
		}
	}
	return true
}
