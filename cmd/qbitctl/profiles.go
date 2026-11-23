package main

import (
	"fmt"
	"strings"

	qbt "github.com/naterator/qbitctl/pkg/client"
	"github.com/spf13/cobra"
)

// newProfileCmd manages named qBittorrent connections without opening a Web
// API session for read-only operations. --profile on ordinary commands is a
// session-only selector; `profile use` changes the startup default.
func newProfileCmd(opts *CLIOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "profile",
		Aliases: []string{"profiles"},
		Short:   "Manage named qBittorrent server profiles",
		GroupID: "setup",
	}

	list := &cobra.Command{
		Use:   "list",
		Short: "List saved server profiles",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			setupOpts := *opts
			setupOpts.Profile = ""
			setup, err := qbt.ReadSetup(setupOpts)
			if err != nil {
				return result(cmd, err)
			}
			profiles, defaultName, err := qbt.ReadProfiles(setup.Path)
			if err != nil {
				return result(cmd, err)
			}
			if len(profiles) == 0 {
				_, err = fmt.Fprintln(cmd.OutOrStdout(), "No saved profiles.")
				return err
			}
			for _, profile := range profiles {
				marker := " "
				if profile.Name == defaultName {
					marker = "*"
				}
				user := strings.TrimSpace(profile.User)
				if user == "" {
					user = "(no user)"
				}
				if _, err := fmt.Fprintf(cmd.OutOrStdout(), "%s %s\t%s (%s)\n", marker, profile.Name, profile.URL, user); err != nil {
					return err
				}
			}
			return nil
		},
	}

	add := &cobra.Command{
		Use:   "add NAME",
		Short: "Authenticate and save a named server profile",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := strings.TrimSpace(args[0])
			if err := qbt.ValidateProfileName(name); err != nil {
				return result(cmd, codedErr(exitBadArgs, err.Error()))
			}
			setupOpts := *opts
			setupOpts.Profile = ""
			setup, err := qbt.ReadSetup(setupOpts)
			if err != nil {
				return result(cmd, err)
			}
			profiles, _, err := qbt.ReadProfiles(setup.Path)
			if err != nil {
				return result(cmd, err)
			}
			for _, profile := range profiles {
				if profile.Name == name {
					return result(cmd, codedErr(exitBadArgs, fmt.Sprintf("profile %q already exists", name)))
				}
			}
			urlValue, userValue := strings.TrimSpace(opts.URL), strings.TrimSpace(opts.User)
			if urlValue == "" {
				urlValue = setup.URL
			}
			if userValue == "" {
				userValue = setup.User
			}
			if _, err := qbt.ValidateConfigURL(urlValue); err != nil {
				return result(cmd, codedErr(exitBadArgs, err.Error()))
			}
			if userValue == "" || opts.Pass == "" {
				return result(cmd, codedErr(exitBadArgs, "profile add requires --user and --pass (or QBITCTL_PASSWORD) unless they are saved in the active profile"))
			}
			app, err := qbt.ConnectAndAddProfileSetup(cmd.Context(), setup.Path, name, qbt.Credentials{URL: urlValue, User: userValue, Pass: opts.Pass}, opts.CAFile)
			if err != nil {
				return result(cmd, err)
			}
			app.Logout()
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "Saved profile %s to %s\n", name, setup.Path)
			return err
		},
	}

	use := &cobra.Command{
		Use:               "use NAME",
		Short:             "Set the startup default profile",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeProfileArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			setupOpts := *opts
			setupOpts.Profile = ""
			setup, err := qbt.ReadSetup(setupOpts)
			if err != nil {
				return result(cmd, err)
			}
			if err := qbt.SetDefaultProfile(setup.Path, strings.TrimSpace(args[0])); err != nil {
				return result(cmd, err)
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "Startup profile: %s\n", strings.TrimSpace(args[0]))
			return err
		},
	}

	remove := &cobra.Command{
		Use:               "remove NAME",
		Aliases:           []string{"rm"},
		Short:             "Remove a saved server profile",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeProfileArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			setupOpts := *opts
			setupOpts.Profile = ""
			setup, err := qbt.ReadSetup(setupOpts)
			if err != nil {
				return result(cmd, err)
			}
			name := strings.TrimSpace(args[0])
			if err := qbt.RemoveProfile(setup.Path, name); err != nil {
				return result(cmd, err)
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "Removed profile %s\n", name)
			return err
		},
	}

	cmd.AddCommand(list, add, use, remove)
	return cmd
}

func completeProfileArgs(cmd *cobra.Command, _ []string, toComplete string) ([]cobra.Completion, cobra.ShellCompDirective) {
	var configPath string
	if flag := cmd.Flag("config"); flag != nil {
		configPath = flag.Value.String()
	}
	setup, err := qbt.ReadSetup(qbt.Options{ConfigPath: configPath})
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	profiles, _, err := qbt.ReadProfiles(setup.Path)
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	values := make([]string, 0, len(profiles))
	for _, profile := range profiles {
		if strings.HasPrefix(profile.Name, toComplete) {
			values = append(values, cobra.CompletionWithDesc(profile.Name, profile.URL))
		}
	}
	return values, cobra.ShellCompDirectiveNoFileComp
}
