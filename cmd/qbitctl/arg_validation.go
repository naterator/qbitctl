package main

import (
	"fmt"

	qbt "github.com/naterator/qbitctl/pkg/client"

	"github.com/spf13/cobra"
)

func validateOutputArgs(cmd *cobra.Command, tmpl, fields string, noHeader bool) error {
	outputFlags := []string{"server-json", "json", "template", "fields"}
	changed := make([]string, 0, len(outputFlags))
	for _, name := range outputFlags {
		if cmd.Flags().Changed(name) {
			changed = append(changed, name)
		}
	}
	if len(changed) > 1 {
		return codedErr(exitBadArgs, "Use only one of --json, --server-json, --template, or --fields")
	}
	if noHeader && !cmd.Flags().Changed("fields") {
		return codedErr(exitBadArgs, "--no-header requires --fields")
	}
	if cmd.Flags().Changed("fields") {
		if _, err := qbt.ParseSelectableTorrentFields(fields); err != nil {
			return codedErr(exitBadArgs, err.Error())
		}
	}
	if cmd.Flags().Changed("template") {
		if err := qbt.ValidateTorrentTemplate(tmpl); err != nil {
			return codedErr(exitBadArgs, fmt.Sprintf("Invalid template: %v", err))
		}
	}
	return nil
}

// validateJSONModeFlags runs from Args, before the root PersistentPreRunE can
// read --pass-stdin input and before login. Cobra's own flag groups are checked
// only after the pre-run hooks.
func validateJSONModeFlags(cmd *cobra.Command) error {
	if cmd.Flags().Changed("server-json") && cmd.Flags().Changed("json") {
		return codedErr(exitBadArgs, "Use only one of --json or --server-json")
	}
	return nil
}

// jsonModeArgs combines a positional-argument check with the JSON mode check
// for commands that offer only --json and --server-json.
func jsonModeArgs(positional cobra.PositionalArgs) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if err := positional(cmd, args); err != nil {
			return err
		}
		return validateJSONModeFlags(cmd)
	}
}

func validateFieldArg(index int, isKnown func(string) bool, canonical func(string) string, help string) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		if len(args) <= index {
			return nil
		}
		field := canonical(args[index])
		if isKnown(field) {
			return nil
		}
		return codedErr(exitBadArgs, fmt.Sprintf("Unknown field %q. Run qbitctl help %s for the field list.", args[index], help))
	}
}

func validateSetArgs(cmd *cobra.Command, args []string) error {
	if err := cobra.ExactArgs(3)(cmd, args); err != nil {
		return err
	}
	field := qbt.CanonicalSetField(args[0])
	if !qbt.IsSetField(field) {
		return codedErr(exitBadArgs, fmt.Sprintf("Unknown field %q. Run qbitctl help set for the field list.", args[0]))
	}
	if err := qbt.ValidateSetValue(field, args[1]); err != nil {
		return codedErr(exitBadArgs, fmt.Sprintf("Invalid value for %s: %v", field, err))
	}
	return nil
}
