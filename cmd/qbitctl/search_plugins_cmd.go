package main

import (
	"fmt"
	"strings"

	qbt "github.com/naterator/qbitctl/pkg/client"
	"github.com/spf13/cobra"
)

// searchPluginManagementInvocation recognizes the management form without
// making the existing `search plugins` query ambiguous. Management actions
// therefore always have an action word: `search plugins list`, `install`,
// `enable`, `disable`, `uninstall`, or `update`.
func searchPluginManagementInvocation(args []string) bool {
	return len(args) >= 2 && strings.EqualFold(strings.TrimSpace(args[0]), "plugins")
}

func validateSearchPluginManagementArgs(cmd *cobra.Command, args []string) error {
	if !searchPluginManagementInvocation(args) {
		return nil
	}
	for _, flag := range []string{"plugin", "timeout", "recent", "list-plugins", "sort", "min-seeders", "min-size", "max-size", "filter", "limit"} {
		if cmd.Flags().Changed(flag) {
			return codedErr(exitBadArgs, fmt.Sprintf("search plugins cannot be combined with --%s", flag))
		}
	}
	action := strings.ToLower(strings.TrimSpace(args[1]))
	if action != "list" && cmd.Flags().Changed("json") {
		return codedErr(exitBadArgs, "--json is only supported with search plugins list")
	}
	want := 0
	switch action {
	case "list", "update":
		want = 2
	case "install", "enable", "disable", "uninstall":
		want = 3
	default:
		return codedErr(exitBadArgs, "unknown search plugin action: "+qbt.SanitizeServerText(args[1]))
	}
	if len(args) != want {
		return codedErr(exitBadArgs, fmt.Sprintf("search plugins %s expects %d argument(s)", action, want-2))
	}
	if want == 3 {
		var err error
		if action == "install" {
			err = qbt.ValidateSearchPluginSource(args[2])
		} else {
			err = qbt.ValidateSearchPluginName(args[2])
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func runSearchPluginManagement(app *App, args []string, jsonOutput bool) error {
	action := strings.ToLower(strings.TrimSpace(args[1]))
	switch action {
	case "list":
		plugins, err := app.SearchPlugins()
		if err != nil {
			return err
		}
		return renderSearchPlugins(app.Stdout, plugins, jsonOutput)
	case "install":
		return app.InstallSearchPlugin(args[2])
	case "enable":
		return app.EnableSearchPlugin(args[2])
	case "disable":
		return app.DisableSearchPlugin(args[2])
	case "uninstall":
		if err := app.UninstallSearchPlugin(args[2]); err != nil {
			return err
		}
		return resetSavedSearchPluginIfRemoved(app, args[2])
	case "update":
		return app.UpdateSearchPlugins()
	default:
		// Args validation catches this before authentication; retain a safe
		// error for callers that invoke this helper directly in tests.
		return codedErr(exitBadArgs, "unknown search plugin action: "+qbt.SanitizeServerText(args[1]))
	}
}

func resetSavedSearchPluginIfRemoved(app *App, removed string) error {
	return app.ResetDefaultSearchPluginIfMatches(removed)
}
