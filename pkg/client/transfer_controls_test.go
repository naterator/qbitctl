package client

import (
	"errors"
	"strings"
	"testing"
)

func TestGlobalTransferControlsUseTransferEndpoints(t *testing.T) {
	server := newQBTestServer(t)
	server.TransferInfo = &TransferInfo{DLRateLimit: 2048, UPRateLimit: 4096}
	server.SpeedLimitsMode = "0"
	app := server.newApp()

	info, err := app.FetchTransferInfo()
	if err != nil {
		t.Fatalf("FetchTransferInfo: %v", err)
	}
	if info.DLRateLimit != 2048 || info.UPRateLimit != 4096 {
		t.Fatalf("transfer info = %#v", info)
	}
	if mode, err := app.FetchSpeedLimitsMode(); err != nil || mode {
		t.Fatalf("FetchSpeedLimitsMode = %v, %v; want normal", mode, err)
	}

	if err := app.SetGlobalDownloadLimit("2k"); err != nil {
		t.Fatalf("SetGlobalDownloadLimit: %v", err)
	}
	if got := server.Forms["/api/v2/transfer/setDownloadLimit"][0].Get("limit"); got != "2048" {
		t.Fatalf("download limit form = %q, want 2048", got)
	}
	if err := app.SetGlobalUploadLimit("unlimited"); err != nil {
		t.Fatalf("SetGlobalUploadLimit: %v", err)
	}
	if got := server.Forms["/api/v2/transfer/setUploadLimit"][0].Get("limit"); got != "0" {
		t.Fatalf("upload limit form = %q, want 0", got)
	}
}

func TestSetSpeedLimitsModeOnlyTogglesWhenNeededAndVerifies(t *testing.T) {
	server := newQBTestServer(t)
	server.SpeedLimitsMode = "0"
	app := server.newApp()

	if err := app.SetSpeedLimitsMode(false); err != nil {
		t.Fatalf("normal no-op: %v", err)
	}
	if got := countPath(server.CallOrder, "/api/v2/transfer/toggleSpeedLimitsMode"); got != 0 {
		t.Fatalf("normal no-op toggles = %d, want 0", got)
	}

	if err := app.SetSpeedLimitsMode(true); err != nil {
		t.Fatalf("alternate toggle: %v", err)
	}
	if got := countPath(server.CallOrder, "/api/v2/transfer/toggleSpeedLimitsMode"); got != 1 {
		t.Fatalf("alternate toggles = %d, want 1", got)
	}
	if mode, err := app.FetchSpeedLimitsMode(); err != nil || !mode {
		t.Fatalf("verified mode = %v, %v; want alternate", mode, err)
	}

	server = newQBTestServer(t)
	server.SpeedLimitsMode = "0"
	// The scripted read after the toggle disagrees with the requested mode.
	server.BodyQueue["/api/v2/transfer/speedLimitsMode"] = []string{"0", "0"}
	app = server.newApp()
	err := app.SetSpeedLimitsMode(true)
	if err == nil {
		t.Fatal("SetSpeedLimitsMode unexpectedly accepted an unverified toggle")
	}
	var coded *CodedError
	if !errors.As(err, &coded) || coded.Code != ExitActionFail {
		t.Fatalf("verification error = %v, want ExitActionFail", err)
	}
}

func TestTransferControlFailuresReturnUsefulCodedErrors(t *testing.T) {
	t.Run("speed mode invalid response", func(t *testing.T) {
		server := newQBTestServer(t)
		server.BodyQueue["/api/v2/transfer/speedLimitsMode"] = []string{"unexpected"}
		err := server.newApp().SetSpeedLimitsMode(false)
		var coded *CodedError
		if !errors.As(err, &coded) || coded.Code != ExitFetchFail {
			t.Fatalf("invalid mode response error = %v, want ExitFetchFail", err)
		}
	})

	t.Run("speed mode initial read failure", func(t *testing.T) {
		server := newQBTestServer(t)
		server.StatusQueue["/api/v2/transfer/speedLimitsMode"] = []int{500}
		err := server.newApp().SetSpeedLimitsMode(true)
		var coded *CodedError
		if !errors.As(err, &coded) || coded.Code != ExitFetchFail {
			t.Fatalf("initial mode error = %v, want ExitFetchFail", err)
		}
	})

	t.Run("toggle failure", func(t *testing.T) {
		server := newQBTestServer(t)
		server.SpeedLimitsMode = "0"
		server.StatusQueue["/api/v2/transfer/toggleSpeedLimitsMode"] = []int{500}
		err := server.newApp().SetSpeedLimitsMode(true)
		var coded *CodedError
		if !errors.As(err, &coded) || coded.Code != ExitActionFail {
			t.Fatalf("toggle error = %v, want ExitActionFail", err)
		}
	})

	t.Run("verification read failure", func(t *testing.T) {
		server := newQBTestServer(t)
		server.SpeedLimitsMode = "0"
		server.StatusQueue["/api/v2/transfer/speedLimitsMode"] = []int{200, 500}
		err := server.newApp().SetSpeedLimitsMode(true)
		var coded *CodedError
		if !errors.As(err, &coded) || coded.Code != ExitFetchFail {
			t.Fatalf("verification read error = %v, want ExitFetchFail", err)
		}
	})

	t.Run("normal verification mismatch", func(t *testing.T) {
		server := newQBTestServer(t)
		server.SpeedLimitsMode = "1"
		server.BodyQueue["/api/v2/transfer/speedLimitsMode"] = []string{"1", "1"}
		err := server.newApp().SetSpeedLimitsMode(false)
		var coded *CodedError
		if !errors.As(err, &coded) || coded.Code != ExitActionFail || !strings.Contains(err.Error(), "normal") {
			t.Fatalf("normal verification error = %v, want normal ExitActionFail", err)
		}
	})

	for _, test := range []struct {
		name     string
		endpoint string
		set      func(*App) error
	}{
		{name: "download limit request", endpoint: "/api/v2/transfer/setDownloadLimit", set: func(app *App) error {
			return app.SetGlobalDownloadLimit("2k")
		}},
		{name: "upload limit request", endpoint: "/api/v2/transfer/setUploadLimit", set: func(app *App) error {
			return app.SetGlobalUploadLimit("2k")
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := newQBTestServer(t)
			server.StatusQueue[test.endpoint] = []int{500}
			err := test.set(server.newApp())
			var coded *CodedError
			if !errors.As(err, &coded) || coded.Code != ExitSetFail {
				t.Fatalf("set failure = %v, want ExitSetFail", err)
			}
		})
	}

	t.Run("ban requires a peer", func(t *testing.T) {
		err := newQBTestServer(t).newApp().BanPeers()
		var coded *CodedError
		if !errors.As(err, &coded) || coded.Code != ExitBadArgs {
			t.Fatalf("empty ban error = %v, want ExitBadArgs", err)
		}
	})

	t.Run("ban request failure", func(t *testing.T) {
		server := newQBTestServer(t)
		server.StatusQueue["/api/v2/transfer/banPeers"] = []int{500}
		err := server.newApp().BanPeers("192.0.2.7:51413")
		var coded *CodedError
		if !errors.As(err, &coded) || coded.Code != ExitActionFail {
			t.Fatalf("ban failure = %v, want ExitActionFail", err)
		}
	})
}

func countPath(paths []string, want string) int {
	count := 0
	for _, path := range paths {
		if path == want {
			count++
		}
	}
	return count
}

func TestGlobalTransferLimitValidation(t *testing.T) {
	server := newQBTestServer(t)
	app := server.newApp()
	for _, value := range []string{"-1", "-2", "not-a-limit"} {
		if err := app.SetGlobalDownloadLimit(value); err == nil {
			t.Errorf("SetGlobalDownloadLimit(%q) unexpectedly succeeded", value)
		}
	}
	if got := len(server.Forms["/api/v2/transfer/setDownloadLimit"]); got != 0 {
		t.Fatalf("invalid limit requests = %d, want 0", got)
	}
}

func TestValidatePeerAddress(t *testing.T) {
	for _, tt := range []struct {
		value string
		valid bool
	}{
		{value: "192.0.2.7:51413", valid: true},
		{value: "[2001:db8::7]:51413", valid: true},
		{value: "192.0.2.7", valid: false},
		{value: "2001:db8::7:51413", valid: false},
		{value: "example.test:51413", valid: false},
		{value: "192.0.2.7:0", valid: false},
		{value: "192.0.2.7:65536", valid: false},
	} {
		t.Run(tt.value, func(t *testing.T) {
			err := ValidatePeerAddress(tt.value)
			if (err == nil) != tt.valid {
				t.Fatalf("ValidatePeerAddress(%q) error = %v, valid = %t", tt.value, err, tt.valid)
			}
		})
	}
}

func TestBanPeersValidatesBeforeSubmittingAndSupportsIPv6(t *testing.T) {
	server := newQBTestServer(t)
	app := server.newApp()
	if err := app.BanPeers("192.0.2.7:51413", "[2001:db8::7]:51413"); err != nil {
		t.Fatalf("BanPeers: %v", err)
	}
	forms := server.Forms["/api/v2/transfer/banPeers"]
	if len(forms) != 1 || forms[0].Get("peers") != "192.0.2.7:51413|[2001:db8::7]:51413" {
		t.Fatalf("ban form = %#v", forms)
	}
	if err := app.BanPeers("not-an-ip:51413"); err == nil {
		t.Fatal("BanPeers accepted a hostname")
	}
	if got := len(server.Forms["/api/v2/transfer/banPeers"]); got != 1 {
		t.Fatalf("invalid ban request count = %d, want 1", got)
	}
}
