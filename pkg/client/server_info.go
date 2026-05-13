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
		return "", codedErrf(ExitFetchFail, "Failed to fetch app version: %v", err)
	}
	return strings.TrimSpace(string(body)), nil
}

func (a *App) fetchAPIVersion() (string, error) {
	body, err := a.client.requestContext(a.context(), http.MethodGet, "/api/v2/app/webapiVersion", nil)
	if err != nil {
		return "", codedErrf(ExitFetchFail, "Failed to fetch API version: %v", err)
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

func (a *App) fetchTransferInfoRaw() ([]byte, error) {
	body, err := a.client.requestContext(a.context(), http.MethodGet, "/api/v2/transfer/info", nil)
	if err != nil {
		return nil, codedErrf(ExitFetchFail, "Failed to fetch transfer info: %v", err)
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

func (a *App) serverInfoRows() ([]labeledRow, error) {
	appVersion, err := a.fetchAppVersion()
	if err != nil {
		return nil, err
	}
	apiVersion, err := a.fetchAPIVersion()
	if err != nil {
		return nil, err
	}
	transfer, err := a.fetchTransferInfo()
	if err != nil {
		return nil, err
	}

	return []labeledRow{
		{label: "qBittorrent", value: appVersion},
		{label: "WebAPI", value: apiVersion},
		{label: "Connection", value: transfer.ConnectionStatus},
		{label: "DHT Nodes", value: strconv.FormatInt(transfer.DHTNodes, 10)},
		{label: "Speed: Download", value: strconv.FormatInt(transfer.DLInfoSpeed, 10)},
		{label: "Speed: Upload", value: strconv.FormatInt(transfer.UPInfoSpeed, 10)},
		{label: "Transferred: Down", value: strconv.FormatInt(transfer.DLInfoData, 10)},
		{label: "Transferred: Up", value: strconv.FormatInt(transfer.UPInfoData, 10)},
		{label: "Limit: Download", value: strconv.FormatInt(transfer.DLRateLimit, 10)},
		{label: "Limit: Upload", value: strconv.FormatInt(transfer.UPRateLimit, 10)},
	}, nil
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
	rows, err := a.serverInfoRows()
	if err != nil {
		return err
	}
	if err := renderBlurbJSON(a.Stdout, rows); err != nil {
		return codedErrf(ExitFetchFail, "Failed to render JSON: %v", err)
	}
	return nil
}
