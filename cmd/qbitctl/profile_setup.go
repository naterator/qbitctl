package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	qbt "github.com/naterator/qbitctl/pkg/client"
	"github.com/spf13/cobra"
)

func newProfileAddCmd(opts *CLIOptions) *cobra.Command {
	var password string
	cmd := &cobra.Command{
		Use:         "add [NAME]",
		Short:       "Authenticate and save a new server profile",
		Long:        "Add a named profile. Supply the server address and username with --url and --user, or enter them at the prompts. Use --password - to read one password line from stdin.",
		Args:        cobra.MaximumNArgs(1),
		Annotations: map[string]string{stdinModeAnnotation: stdinProfilePassword},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return cmd.Help()
			}
			return result(cmd, addProfile(cmd, *opts, args[0], password))
		},
	}
	cmd.Flags().StringVar(&password, "password", "", "Read from stdin with '-' or supply a value (visible in process arguments)")
	return cmd
}

func addProfile(cmd *cobra.Command, opts CLIOptions, name, password string) error {
	name, err := qbt.ValidateAndNormalizeProfileName(name)
	if err != nil {
		return err
	}
	flags := cmd.Flags()
	passwordOption := flags.Changed("password")
	if passwordOption && (flags.Changed("pass") || flags.Changed("pass-stdin")) {
		return fmt.Errorf("--password cannot be combined with --pass or --pass-stdin")
	}
	if passwordOption && password == "" {
		return qbt.ErrSetupPasswordRequired
	}
	if flags.Changed("pass") && flags.Changed("pass-stdin") {
		return fmt.Errorf("--pass and --pass-stdin are mutually exclusive")
	}

	setup, err := qbt.ReadSetup(qbt.Options{ConfigPath: opts.ConfigPath})
	if err != nil {
		return err
	}
	profiles, _, err := qbt.ReadProfiles(setup.Path)
	if err != nil {
		return err
	}
	for _, profile := range profiles {
		if profile.Name == name {
			return fmt.Errorf("profile %q already exists", name)
		}
	}

	creds := qbt.Credentials{URL: strings.TrimSpace(opts.URL), User: strings.TrimSpace(opts.User)}
	if creds.URL != "" {
		if _, err := qbt.ValidateConfigURL(creds.URL); err != nil {
			return err
		}
	}
	interactive := tuiTerminal(cmd.InOrStdin()) && tuiTerminal(cmd.ErrOrStderr())
	passStdin, _ := flags.GetBool("pass-stdin")
	explicit := passwordOption || flags.Changed("pass") || passStdin
	useEnvironment := os.Getenv("QBITCTL_PASSWORD") != "" &&
		(!interactive || flags.Changed("url") || flags.Changed("user"))
	if explicit || useEnvironment {
		err = collectProfilePassword(cmd, &opts, &creds, setup, passwordOption, password, passStdin)
	} else if interactive {
		in, out := cmd.InOrStdin(), cmd.ErrOrStderr()
		creds, err = qbt.PromptProfileCredentials(bufio.NewReader(in), out, makeHiddenInput(in, out), creds, setup)
	} else {
		return fmt.Errorf("profile add requires an interactive terminal or --url, --user, and --password - (or QBITCTL_PASSWORD)")
	}
	if err != nil {
		return err
	}
	app, err := qbt.ConnectAndAddProfileSetup(cmd.Context(), setup.Path, name, creds, opts.CAFile)
	if err != nil {
		return err
	}
	app.Logout()
	_, err = fmt.Fprintf(cmd.OutOrStdout(), "Saved profile %s to %s\n", name, setup.Path)
	return err
}

func collectProfilePassword(cmd *cobra.Command, opts *CLIOptions, creds *qbt.Credentials, setup qbt.Setup, passwordOption bool, password string, passStdin bool) error {
	if creds.URL == "" {
		creds.URL = setup.URL
	}
	if creds.User == "" {
		creds.User = setup.User
	}
	if _, err := qbt.ValidateConfigURL(creds.URL); err != nil {
		return err
	}
	if strings.TrimSpace(creds.User) == "" {
		return fmt.Errorf("--user is required when no username is saved")
	}
	if passwordOption {
		if password == "-" {
			in := cmd.InOrStdin()
			if tuiTerminal(in) {
				return fmt.Errorf("--password - expects a password on piped input")
			}
			var err error
			password, err = readPasswordLine(in)
			if err != nil {
				return err
			}
		}
		creds.Pass = password
	} else {
		if err := resolvePassword(cmd, opts, passStdin); err != nil {
			return err
		}
		creds.Pass = opts.Pass
	}
	if creds.Pass == "" {
		return qbt.ErrSetupPasswordRequired
	}
	return nil
}

func newProfileSetupCmd(opts *CLIOptions) *cobra.Command {
	return &cobra.Command{
		Use:               "setup [NAME]",
		Short:             "Run the text setup for an existing connection or legacy config",
		Args:              cobra.MaximumNArgs(1),
		ValidArgsFunction: completeProfileArgs,
		Annotations:       map[string]string{stdinModeAnnotation: stdinInteractive},
		RunE: func(cmd *cobra.Command, args []string) error {
			profile := opts.Profile
			if len(args) == 1 {
				if profile != "" && profile != args[0] {
					return result(cmd, fmt.Errorf("specify the profile once, as NAME or --profile"))
				}
				profile = args[0]
			}
			in, out := cmd.InOrStdin(), cmd.ErrOrStderr()
			path, err := qbt.InteractiveConfigProfileContext(cmd.Context(), in, out, makeHiddenInput(in, out), opts.ConfigPath, profile)
			if err != nil {
				return result(cmd, err)
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "Saved config to %s\n", path)
			return err
		},
	}
}

func newProfileWriteCmd(opts *CLIOptions) *cobra.Command {
	var output string
	cmd := &cobra.Command{
		Use:   "write",
		Short: "Write connection settings without authenticating",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return result(cmd, qbt.ExecuteConfigWrite(*opts, output, cmd.ErrOrStderr()))
		},
	}
	cmd.Flags().StringVarP(&output, "output", "o", "", "Path to write config.json")
	return cmd
}
