package main

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	qbt "github.com/naterator/qbitctl/pkg/client"
	"github.com/spf13/cobra"
)

// newTransferCmd exposes qBittorrent's global transfer controls. The root
// command registration intentionally stays with the main CLI owner.
//
//	qbitctl transfer info [--json]
//	qbitctl transfer set download <bytes|Nk|0|unlimited>
//	qbitctl transfer set upload <bytes|Nk|0|unlimited>
//	qbitctl transfer mode [normal|alternate]
//	qbitctl transfer ban-peer <IPv4:port|[IPv6]:port...>
func newTransferCmd(opts *CLIOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "transfer",
		Aliases: []string{"tr", "speed"},
		Short:   "View and change global transfer controls",
		GroupID: "control",
		Args:    cobra.NoArgs,
	}
	cmd.AddCommand(
		newTransferInfoCmd(opts),
		newTransferSetCmd(opts),
		newTransferModeCmd(opts),
		newTransferBanPeerCmd(opts),
	)
	return cmd
}

type transferInfoOutput struct {
	qbt.TransferInfo
	SpeedLimitsMode string `json:"speed_limits_mode"`
}

func newTransferInfoCmd(opts *CLIOptions) *cobra.Command {
	jsonOutput := false
	cmd := &cobra.Command{
		Use:     "info",
		Aliases: []string{"i"},
		Short:   "Show global transfer rates, limits, and speed mode",
		Args:    cobra.NoArgs,
		RunE: authenticatedRun(opts, func(app *App, _ []string) error {
			info, err := app.FetchTransferInfo()
			if err != nil {
				return err
			}
			alternate, err := app.FetchSpeedLimitsMode()
			if err != nil {
				return err
			}
			mode := "normal"
			if alternate {
				mode = "alternate"
			}
			if jsonOutput {
				if err := json.NewEncoder(app.Stdout).Encode(transferInfoOutput{TransferInfo: info, SpeedLimitsMode: mode}); err != nil {
					return codedErr(exitFetchFail, fmt.Sprintf("Failed to render transfer info: %v", err))
				}
				return nil
			}
			renderTransferInfo(app.Stdout, info, mode, app.DataUnits)
			return nil
		}),
	}
	cmd.Flags().BoolVarP(&jsonOutput, "json", "j", false, "Output transfer info as JSON")
	return cmd
}

func newTransferSetCmd(opts *CLIOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "set",
		Short: "Set an active global transfer limit",
		Args:  cobra.NoArgs,
	}
	for _, child := range []*cobra.Command{
		{
			Use:   "download <bytes|Nk|0|unlimited>",
			Short: "Set the global download limit",
			Args:  cobra.ExactArgs(1),
			RunE: authenticatedRun(opts, func(app *App, args []string) error {
				return app.SetGlobalDownloadLimit(args[0])
			}),
		},
		{
			Use:   "upload <bytes|Nk|0|unlimited>",
			Short: "Set the global upload limit",
			Args:  cobra.ExactArgs(1),
			RunE: authenticatedRun(opts, func(app *App, args []string) error {
				return app.SetGlobalUploadLimit(args[0])
			}),
		},
	} {
		cmd.AddCommand(child)
	}
	return cmd
}

func newTransferModeCmd(opts *CLIOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:       "mode [normal|alternate]",
		Aliases:   []string{"schedule"},
		Short:     "Read or set the active speed-limit mode",
		Args:      cobra.MaximumNArgs(1),
		ValidArgs: []string{"normal", "alternate"},
		RunE: authenticatedRun(opts, func(app *App, args []string) error {
			if len(args) == 0 {
				alternate, err := app.FetchSpeedLimitsMode()
				if err != nil {
					return err
				}
				if alternate {
					fmt.Fprintln(app.Stdout, "alternate")
				} else {
					fmt.Fprintln(app.Stdout, "normal")
				}
				return nil
			}
			mode := strings.ToLower(strings.TrimSpace(args[0]))
			var alternate bool
			switch mode {
			case "normal":
			case "alternate":
				alternate = true
			default:
				return codedErr(exitBadArgs, "mode must be normal or alternate")
			}
			if err := app.SetSpeedLimitsMode(alternate); err != nil {
				return err
			}
			fmt.Fprintln(app.Stdout, mode)
			return nil
		}),
	}
	return cmd
}

func newTransferBanPeerCmd(opts *CLIOptions) *cobra.Command {
	return &cobra.Command{
		Use:     "ban-peer <IPv4:port|[IPv6]:port...>",
		Aliases: []string{"ban"},
		Short:   "Ban one or more peers",
		Args:    cobra.MinimumNArgs(1),
		RunE: authenticatedRun(opts, func(app *App, args []string) error {
			return app.BanPeers(args...)
		}),
	}
}

func renderTransferInfo(w io.Writer, info qbt.TransferInfo, mode string, units qbt.DataUnits) {
	formatLimit := func(value int64) string {
		if value <= 0 {
			return "unlimited"
		}
		return units.FormatRate(value)
	}
	fmt.Fprintf(w, "Speed: Download\t%s\n", units.FormatRate(info.DLInfoSpeed))
	fmt.Fprintf(w, "Speed: Upload\t%s\n", units.FormatRate(info.UPInfoSpeed))
	fmt.Fprintf(w, "Transferred: Down\t%s\n", units.FormatSize(info.DLInfoData))
	fmt.Fprintf(w, "Transferred: Up\t%s\n", units.FormatSize(info.UPInfoData))
	fmt.Fprintf(w, "Limit: Download\t%s\n", formatLimit(info.DLRateLimit))
	fmt.Fprintf(w, "Limit: Upload\t%s\n", formatLimit(info.UPRateLimit))
	fmt.Fprintf(w, "Speed limits mode\t%s\n", mode)
}
