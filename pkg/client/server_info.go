package client

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

const (
	minQbittorrentVersion = "5.0.0"
	minWebAPIVersion      = "2.11.0"
)

func (a *App) fetchAppVersion() (string, error) {
	body, err := a.client.requestContext(a.context(), http.MethodGet, "/api/v2/app/version", nil)
	if err != nil {
		return "", codedErrWrapf(ExitFetchFail, err, "Failed to fetch app version: %v", err)
	}
	return strings.TrimSpace(string(body)), nil
}

func (a *App) fetchAPIVersion() (string, error) {
	body, err := a.client.requestContext(a.context(), http.MethodGet, "/api/v2/app/webapiVersion", nil)
	if err != nil {
		return "", codedErrWrapf(ExitFetchFail, err, "Failed to fetch API version: %v", err)
	}
	return strings.TrimSpace(string(body)), nil
}

func (a *App) CheckCompatibility() error {
	appVersion, err := a.fetchAppVersion()
	if err != nil {
		return err
	}
	apiVersion, err := a.fetchAPIVersion()
	if err != nil {
		return err
	}
	if compareDottedVersion(appVersion, minQbittorrentVersion) < 0 {
		return codedErrf(ExitLoginFail, "Unsupported qBittorrent version %s; qbitctl requires qBittorrent %s or later", appVersion, minQbittorrentVersion)
	}
	if compareDottedVersion(apiVersion, minWebAPIVersion) < 0 {
		return codedErrf(ExitLoginFail, "Unsupported qBittorrent WebAPI version %s; qbitctl requires WebAPI %s or later", apiVersion, minWebAPIVersion)
	}
	return nil
}

func compareDottedVersion(a, b string) int {
	aParts := numericVersionParts(a)
	bParts := numericVersionParts(b)
	maxLen := len(aParts)
	if len(bParts) > maxLen {
		maxLen = len(bParts)
	}
	for i := 0; i < maxLen; i++ {
		var av, bv int
		if i < len(aParts) {
			av = aParts[i]
		}
		if i < len(bParts) {
			bv = bParts[i]
		}
		switch {
		case av > bv:
			return 1
		case av < bv:
			return -1
		}
	}
	return 0
}

func numericVersionParts(version string) []int {
	version = strings.TrimSpace(strings.TrimPrefix(version, "v"))
	rawParts := strings.Split(version, ".")
	parts := make([]int, 0, len(rawParts))
	for _, part := range rawParts {
		digits := strings.Builder{}
		for _, r := range part {
			if r < '0' || r > '9' {
				break
			}
			digits.WriteRune(r)
		}
		if digits.Len() == 0 {
			parts = append(parts, 0)
			continue
		}
		n, err := strconv.Atoi(digits.String())
		if err != nil {
			parts = append(parts, 0)
			continue
		}
		parts = append(parts, n)
	}
	return parts
}

func isDottedVersion(version string) bool {
	version = strings.TrimPrefix(strings.TrimSpace(version), "v")
	parts := strings.Split(version, ".")
	if len(parts) < 2 {
		return false
	}
	for _, part := range parts {
		if part == "" || part[0] < '0' || part[0] > '9' {
			return false
		}
		if _, err := strconv.Atoi(part); err != nil {
			return false
		}
	}
	return true
}

func (a *App) fetchTransferInfoRaw() ([]byte, error) {
	body, err := a.client.requestContext(a.context(), http.MethodGet, "/api/v2/transfer/info", nil)
	if err != nil {
		return nil, codedErrWrapf(ExitFetchFail, err, "Failed to fetch transfer info: %v", err)
	}
	return body, nil
}

func (a *App) fetchTransferInfo() (TransferInfo, error) {
	body, err := a.fetchTransferInfoRaw()
	if err != nil {
		return TransferInfo{}, err
	}

	var info TransferInfo
	if err := json.Unmarshal(body, &info); err != nil {
		return TransferInfo{}, codedErrf(ExitFetchFail, "Invalid JSON from transfer info endpoint")
	}
	return info, nil
}

func (a *App) ShowServerInfoJSON() error {
	body, err := a.fetchTransferInfoRaw()
	if err != nil {
		return err
	}
	fmt.Fprintln(a.Stdout, string(body))
	return nil
}

func (a *App) fetchServerInfo() (string, string, TransferInfo, error) {
	appVersion, err := a.fetchAppVersion()
	if err != nil {
		return "", "", TransferInfo{}, err
	}
	apiVersion, err := a.fetchAPIVersion()
	if err != nil {
		return "", "", TransferInfo{}, err
	}
	transfer, err := a.fetchTransferInfo()
	if err != nil {
		return "", "", TransferInfo{}, err
	}
	return appVersion, apiVersion, transfer, nil
}

func serverInfoRowsForValues(
	appVersion, apiVersion string,
	transfer TransferInfo,
	size func(int64) string,
	rate func(int64) string,
	limit func(int64) string,
) []labeledRow {

	return []labeledRow{
		{label: "qBittorrent", value: appVersion},
		{label: "WebAPI", value: apiVersion},
		{label: "Connection", value: transfer.ConnectionStatus},
		{label: "DHT Nodes", value: strconv.FormatInt(transfer.DHTNodes, 10)},
		{label: "Speed: Download", value: rate(transfer.DLInfoSpeed)},
		{label: "Speed: Upload", value: rate(transfer.UPInfoSpeed)},
		{label: "Transferred: Down", value: size(transfer.DLInfoData)},
		{label: "Transferred: Up", value: size(transfer.UPInfoData)},
		{label: "Limit: Download", value: limit(transfer.DLRateLimit)},
		{label: "Limit: Upload", value: limit(transfer.UPRateLimit)},
	}
}

func (a *App) serverInfoRows() ([]labeledRow, error) {
	appVersion, apiVersion, transfer, err := a.fetchServerInfo()
	if err != nil {
		return nil, err
	}
	return serverInfoRowsForValues(
		appVersion,
		apiVersion,
		transfer,
		a.DataUnits.FormatSize,
		a.DataUnits.FormatRate,
		func(value int64) string {
			if value == 0 || value == -1 {
				return "unlimited"
			}
			return a.DataUnits.FormatRate(value)
		},
	), nil
}

func (a *App) serverInfoRawRows() ([]labeledRow, error) {
	appVersion, apiVersion, transfer, err := a.fetchServerInfo()
	if err != nil {
		return nil, err
	}
	formatRaw := func(value int64) string { return strconv.FormatInt(value, 10) }
	return serverInfoRowsForValues(appVersion, apiVersion, transfer, formatRaw, formatRaw, formatRaw), nil
}

func (a *App) ShowServerInfo() error {
	rows, err := a.serverInfoRows()
	if err != nil {
		return err
	}
	renderBlurb(a.Stdout, rows)
	return nil
}

func (a *App) ShowServerInfoAsJSON() error {
	rows, err := a.serverInfoRawRows()
	if err != nil {
		return err
	}
	if err := renderBlurbJSON(a.Stdout, rows); err != nil {
		return codedErrf(ExitFetchFail, "Failed to render JSON: %v", err)
	}
	return nil
}

// ServerDisplayFields returns the human-facing server and transfer fields
// used by the terminal UI. Raw JSON remains available through
// ShowServerInfoJSON and ShowServerInfoAsJSON.
func (a *App) ServerDisplayFields() (map[string]string, error) {
	rows, err := a.serverInfoRows()
	if err != nil {
		return nil, err
	}
	return rowsToMap(rows), nil
}
