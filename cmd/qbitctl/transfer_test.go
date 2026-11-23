package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

	qbt "github.com/naterator/qbitctl/pkg/client"
)

func executeTransferTestCommand(t *testing.T, app *App, args ...string) (string, error) {
	t.Helper()
	cmd := newTransferCmd(&CLIOptions{})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)
	cmd.SetContext(withBatchApp(context.Background(), app))
	err := cmd.Execute()
	return out.String(), err
}

func newTransferCLIApp(t *testing.T, server *qbTestServer) *qbt.App {
	t.Helper()
	withQBServerClientFactory(t, server)
	creds := server.creds()
	app, err := qbt.NewClientWithOutput(context.Background(), &qbt.Options{
		URL:  creds.URL,
		User: creds.User,
		Pass: creds.Pass,
	}, nil, nil)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	t.Cleanup(app.Logout)
	return app
}

type transferFailWriter struct{}

func (transferFailWriter) Write([]byte) (int, error) {
	return 0, errors.New("transfer output failed")
}

func executeTransferTestCommandWithWriter(t *testing.T, app *App, writer io.Writer, args ...string) error {
	t.Helper()
	cmd := newTransferCmd(&CLIOptions{})
	cmd.SetOut(writer)
	cmd.SetErr(io.Discard)
	cmd.SetArgs(args)
	cmd.SetContext(withBatchApp(context.Background(), app))
	return cmd.Execute()
}

func TestTransferCommandsUseGlobalEndpoints(t *testing.T) {
	server := newQBTestServer(t)
	server.TransferInfo = &qbt.TransferInfo{DLRateLimit: 2048, UPRateLimit: 4096}
	server.SpeedLimitsMode = "0"
	withQBServerClientFactory(t, server)
	app, err := qbt.NewClientWithOutput(context.Background(), &qbt.Options{
		URL:  server.creds().URL,
		User: server.creds().User,
		Pass: server.creds().Pass,
	}, nil, nil)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	defer app.Logout()

	output, err := executeTransferTestCommand(t, app, "info", "--json")
	if err != nil {
		t.Fatalf("transfer info: %v", err)
	}
	var info struct {
		DLRateLimit    int64  `json:"dl_rate_limit"`
		UPRateLimit    int64  `json:"up_rate_limit"`
		SpeedLimitMode string `json:"speed_limits_mode"`
	}
	if err := json.Unmarshal([]byte(output), &info); err != nil {
		t.Fatalf("transfer info JSON = %q: %v", output, err)
	}
	if info.DLRateLimit != 2048 || info.UPRateLimit != 4096 || info.SpeedLimitMode != "normal" {
		t.Fatalf("transfer info = %#v", info)
	}

	if _, err := executeTransferTestCommand(t, app, "set", "download", "2k"); err != nil {
		t.Fatalf("transfer set download: %v", err)
	}
	if got := server.Forms["/api/v2/transfer/setDownloadLimit"][0].Get("limit"); got != "2048" {
		t.Fatalf("download form = %q, want 2048", got)
	}
	if _, err := executeTransferTestCommand(t, app, "set", "upload", "unlimited"); err != nil {
		t.Fatalf("transfer set upload: %v", err)
	}
	if got := server.Forms["/api/v2/transfer/setUploadLimit"][0].Get("limit"); got != "0" {
		t.Fatalf("upload form = %q, want 0", got)
	}

	output, err = executeTransferTestCommand(t, app, "mode")
	if err != nil || output != "normal\n" {
		t.Fatalf("transfer mode output = %q, err = %v", output, err)
	}
	output, err = executeTransferTestCommand(t, app, "mode", "alternate")
	if err != nil || output != "alternate\n" {
		t.Fatalf("transfer mode alternate output = %q, err = %v", output, err)
	}
	if got := len(server.CallOrder); got != 3 {
		t.Fatalf("transfer mutation call order = %#v", server.CallOrder)
	}
	for i, want := range []string{"/api/v2/transfer/setDownloadLimit", "/api/v2/transfer/setUploadLimit", "/api/v2/transfer/toggleSpeedLimitsMode"} {
		if server.CallOrder[i] != want {
			t.Fatalf("transfer mutation %d = %q, want %q", i, server.CallOrder[i], want)
		}
	}

	if _, err := executeTransferTestCommand(t, app, "ban-peer", "192.0.2.7:51413", "[2001:db8::7]:51413"); err != nil {
		t.Fatalf("transfer ban-peer: %v", err)
	}
	if got := server.Forms["/api/v2/transfer/banPeers"][0].Get("peers"); got != "192.0.2.7:51413|[2001:db8::7]:51413" {
		t.Fatalf("ban peers form = %q", got)
	}
}

func TestTransferInfoHumanOutputAndCommandFailures(t *testing.T) {
	t.Run("human output", func(t *testing.T) {
		server := newQBTestServer(t)
		server.TransferInfo = &qbt.TransferInfo{
			DLInfoSpeed: 2048, UPInfoSpeed: 1024, DLInfoData: 4096, UPInfoData: 8192,
			DLRateLimit: 16384, UPRateLimit: 0,
		}
		server.SpeedLimitsMode = "1"
		app := newTransferCLIApp(t, server)
		output, err := executeTransferTestCommand(t, app, "info")
		if err != nil {
			t.Fatalf("human transfer info: %v", err)
		}
		for _, want := range []string{
			"Speed: Download\t2.0 KiB/s",
			"Speed: Upload\t1.0 KiB/s",
			"Transferred: Down\t4.0 KiB",
			"Transferred: Up\t8.0 KiB",
			"Limit: Download\t16.0 KiB/s",
			"Limit: Upload\tunlimited",
			"Speed limits mode\talternate",
		} {
			if !strings.Contains(output, want) {
				t.Fatalf("human transfer info %q omitted %q", output, want)
			}
		}
	})

	t.Run("transfer info request failure", func(t *testing.T) {
		server := newQBTestServer(t)
		server.StatusQueue["/api/v2/transfer/info"] = []int{500}
		app := newTransferCLIApp(t, server)
		_, err := executeTransferTestCommand(t, app, "info")
		var exit exitError
		if !errors.As(err, &exit) || exit.code != exitFetchFail {
			t.Fatalf("transfer info error = %v, want ExitFetchFail", err)
		}
	})

	t.Run("speed mode request failure", func(t *testing.T) {
		server := newQBTestServer(t)
		server.StatusQueue["/api/v2/transfer/speedLimitsMode"] = []int{500}
		app := newTransferCLIApp(t, server)
		_, err := executeTransferTestCommand(t, app, "mode")
		var exit exitError
		if !errors.As(err, &exit) || exit.code != exitFetchFail {
			t.Fatalf("speed mode error = %v, want ExitFetchFail", err)
		}
	})

	t.Run("invalid mode argument", func(t *testing.T) {
		server := newQBTestServer(t)
		app := newTransferCLIApp(t, server)
		_, err := executeTransferTestCommand(t, app, "mode", "bogus")
		var exit exitError
		if !errors.As(err, &exit) || exit.code != exitBadArgs {
			t.Fatalf("invalid mode error = %v, want ExitBadArgs", err)
		}
		if len(server.CallOrder) != 0 {
			t.Fatalf("invalid mode sent requests: %#v", server.CallOrder)
		}
	})

	t.Run("mode mutation failure", func(t *testing.T) {
		server := newQBTestServer(t)
		server.SpeedLimitsMode = "0"
		server.StatusQueue["/api/v2/transfer/toggleSpeedLimitsMode"] = []int{500}
		app := newTransferCLIApp(t, server)
		_, err := executeTransferTestCommand(t, app, "mode", "alternate")
		var exit exitError
		if !errors.As(err, &exit) || exit.code != exitActionFail {
			t.Fatalf("mode mutation error = %v, want ExitActionFail", err)
		}
	})

	t.Run("json output failure", func(t *testing.T) {
		server := newQBTestServer(t)
		app := newTransferCLIApp(t, server)
		err := executeTransferTestCommandWithWriter(t, app, transferFailWriter{}, "info", "--json")
		var exit exitError
		if !errors.As(err, &exit) || exit.code != exitFetchFail {
			t.Fatalf("JSON output error = %v, want ExitFetchFail", err)
		}
	})
}

func TestTransferCommandsRejectInvalidValues(t *testing.T) {
	server := newQBTestServer(t)
	app := newTransferCLIApp(t, server)
	for _, args := range [][]string{
		{"set", "download", "not-a-limit"},
		{"set", "upload", "1M"},
		{"ban-peer", "not-an-ip:51413"},
	} {
		_, err := executeTransferTestCommand(t, app, args...)
		var exit exitError
		if !errors.As(err, &exit) || exit.code != exitBadArgs {
			t.Fatalf("transfer %v error = %v, want ExitBadArgs", args, err)
		}
	}
	if len(server.Forms["/api/v2/transfer/setDownloadLimit"]) != 0 || len(server.Forms["/api/v2/transfer/setUploadLimit"]) != 0 || len(server.Forms["/api/v2/transfer/banPeers"]) != 0 {
		t.Fatal("invalid transfer values sent mutation requests")
	}
}
