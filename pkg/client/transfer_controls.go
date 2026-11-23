package client

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// FetchTransferInfo returns qBittorrent's current global transfer counters and
// limits. The existing server-info renderer uses the same endpoint; this
// exported form is useful to commands that need the structured values.
func (a *App) FetchTransferInfo() (TransferInfo, error) {
	return a.fetchTransferInfo()
}

// FetchSpeedLimitsMode reports whether qBittorrent is using its alternate
// speed-limit schedule. qBittorrent returns this endpoint as plain text ("0"
// or "1"), rather than a JSON value.
func (a *App) FetchSpeedLimitsMode() (bool, error) {
	body, err := a.client.requestContext(a.context(), http.MethodGet, "/api/v2/transfer/speedLimitsMode", nil)
	if err != nil {
		return false, codedErrWrapf(ExitFetchFail, err, "Failed to fetch speed limits mode: %v", err)
	}
	switch strings.ToLower(strings.Trim(strings.TrimSpace(string(body)), `"`)) {
	case "1", "true":
		return true, nil
	case "0", "false":
		return false, nil
	default:
		return false, codedErrf(ExitFetchFail, "Invalid speed limits mode from qBittorrent")
	}
}

// SetSpeedLimitsMode makes the requested normal/alternate mode active. The
// Web API exposes a toggle rather than requiring callers to know the current
// state, so read first, toggle only when needed, and read back to verify the
// result. This also avoids an unnecessary mutation when the desired mode is
// already active.
func (a *App) SetSpeedLimitsMode(alternate bool) error {
	current, err := a.FetchSpeedLimitsMode()
	if err != nil {
		return err
	}
	if current == alternate {
		return nil
	}
	if _, err := a.client.requestContext(a.context(), http.MethodPost, "/api/v2/transfer/toggleSpeedLimitsMode", nil); err != nil {
		return codedErrWrapf(ExitActionFail, err, "Failed to toggle speed limits mode: %v", err)
	}
	verified, err := a.FetchSpeedLimitsMode()
	if err != nil {
		return err
	}
	if verified != alternate {
		want := "normal"
		if alternate {
			want = "alternate"
		}
		return codedErrf(ExitActionFail, "qBittorrent did not apply %s speed limits mode", want)
	}
	return nil
}

func parseGlobalSpeedLimit(value string) (int64, error) {
	value = strings.TrimSpace(value)
	switch strings.ToLower(value) {
	case "unlimited", "none", "off", "0":
		// qBittorrent's global limit endpoints use zero for unlimited.
		return 0, nil
	}
	parsed, err := parseLimit(value)
	if err != nil || parsed < 0 {
		return 0, fmt.Errorf("limit must be non-negative bytes per second, 0, or unlimited")
	}
	return parsed, nil
}

func (a *App) setGlobalSpeedLimit(value, endpoint, label string) error {
	limit, err := parseGlobalSpeedLimit(value)
	if err != nil {
		return codedErrf(ExitBadArgs, "Invalid %s: %s", label, value)
	}
	params := url.Values{"limit": {strconv.FormatInt(limit, 10)}}
	if _, err := a.client.requestContext(a.context(), http.MethodPost, endpoint, params); err != nil {
		return codedErrWrapf(ExitSetFail, err, "Failed to set %s: %v", label, err)
	}
	return nil
}

// SetGlobalDownloadLimit sets qBittorrent's active global download limit in
// bytes per second. Zero and the unlimited aliases disable the limit.
func (a *App) SetGlobalDownloadLimit(value string) error {
	return a.setGlobalSpeedLimit(value, "/api/v2/transfer/setDownloadLimit", "global download limit")
}

// SetGlobalUploadLimit sets qBittorrent's active global upload limit in bytes
// per second. Zero and the unlimited aliases disable the limit.
func (a *App) SetGlobalUploadLimit(value string) error {
	return a.setGlobalSpeedLimit(value, "/api/v2/transfer/setUploadLimit", "global upload limit")
}

// ValidatePeerAddress accepts the IP:port syntax required by qBittorrent's
// transfer/banPeers endpoint. IPv6 addresses must use the usual [addr]:port
// form. Hostnames, I2P destinations, missing ports, and invalid port ranges
// are intentionally rejected so a TUI confirmation cannot silently submit a
// peer qBittorrent will ignore.
func ValidatePeerAddress(value string) error {
	value = strings.TrimSpace(value)
	host, portText, err := net.SplitHostPort(value)
	if err != nil || host == "" || portText == "" || net.ParseIP(host) == nil {
		return fmt.Errorf("peer must be an IPv4:port or [IPv6]:port address")
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 {
		return fmt.Errorf("peer port must be between 1 and 65535")
	}
	return nil
}

// BanPeers asks qBittorrent to ban one or more peers. All values are validated
// before the request is sent; the API accepts the addresses in one pipe-
// separated form field.
func (a *App) BanPeers(peers ...string) error {
	if len(peers) == 0 {
		return codedErrf(ExitBadArgs, "At least one peer address is required")
	}
	validated := make([]string, 0, len(peers))
	for _, peer := range peers {
		peer = strings.TrimSpace(peer)
		if err := ValidatePeerAddress(peer); err != nil {
			return codedErrf(ExitBadArgs, "Invalid peer %q: %v", peer, err)
		}
		validated = append(validated, peer)
	}
	params := url.Values{"peers": {strings.Join(validated, "|")}}
	if _, err := a.client.requestContext(a.context(), http.MethodPost, "/api/v2/transfer/banPeers", params); err != nil {
		return codedErrWrapf(ExitActionFail, err, "Failed to ban peers: %v", err)
	}
	return nil
}
