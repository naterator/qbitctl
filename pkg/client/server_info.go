package client

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
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
