package client

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sync/atomic"
)

var Version = "dev"

const (
	ExitOK         = 0
	ExitLoginFail  = 1
	ExitFetchFail  = 2
	ExitSetFail    = 3
	ExitBadArgs    = 4
	ExitFile       = 5
	ExitActionFail = 6
)

type Credentials struct {
	URL  string
	User string
	Pass string
}

// TorrentInfo uses minutes for qBittorrent's share time limits. The wire
// sentinels are -1 for no limit and -2 for the global limit.
type TorrentInfo struct {
	Name              string  `json:"name"`
	Hash              string  `json:"hash"`
	Tags              string  `json:"tags"`
	Category          string  `json:"category"`
	UpLimit           int64   `json:"up_limit"`
	DLLimit           int64   `json:"dl_limit"`
	ContentPath       string  `json:"content_path"`
	Tracker           string  `json:"tracker"`
	RatioLimit        float64 `json:"ratio_limit"`
	SeedingTime       int64   `json:"seeding_time"`
	SeedingTimeLimit  int64   `json:"seeding_time_limit"`
	SequentialDL      bool    `json:"seq_dl"`
	AutoTMM           bool    `json:"auto_tmm"`
	SuperSeeding      bool    `json:"super_seeding"`
	Private           bool    `json:"private"`
	State             string  `json:"state"`
	Ratio             float64 `json:"ratio"`
	UpSpeed           int64   `json:"upspeed"`
	DLSpeed           int64   `json:"dlspeed"`
	Uploaded          int64   `json:"uploaded"`
	Downloaded        int64   `json:"downloaded"`
	Size              int64   `json:"size"`
	TotalSize         int64   `json:"total_size"`
	TotalSeeders      *int64  `json:"num_complete,omitempty"` // Swarm count, nil when omitted.
	ETA               int64   `json:"eta"`
	Progress          float64 `json:"progress"`
	InactiveSeedingTL int64   `json:"inactive_seeding_time_limit"`
	ShareLimitsMode   string  `json:"share_limits_mode,omitempty"`
	ShareLimitAction  string  `json:"share_limit_action,omitempty"`
}

type TrackerEntry struct {
	URL string `json:"url"`
}

type TransferInfo struct {
	DLInfoSpeed      int64  `json:"dl_info_speed"`
	UPInfoSpeed      int64  `json:"up_info_speed"`
	DLInfoData       int64  `json:"dl_info_data"`
	UPInfoData       int64  `json:"up_info_data"`
	DLRateLimit      int64  `json:"dl_rate_limit"`
	UPRateLimit      int64  `json:"up_rate_limit"`
	DHTNodes         int64  `json:"dht_nodes"`
	ConnectionStatus string `json:"connection_status"`
}

type Client struct {
	creds      Credentials
	httpClient *http.Client
	stderr     io.Writer

	// session is replaced atomically after login. The snapshot is immutable so
	// cleanup can still use the last known session while a re-login is in
	// flight or when a response body read fails.
	session atomic.Pointer[clientSession]
}

type sessionCookie struct {
	name  string
	value string
}

type clientSession struct {
	logoutURL *url.URL
	cookies   []sessionCookie
}

type App struct {
	client *Client
	creds  Credentials
	// configPath is the file selected while resolving credentials. It remains
	// useful to callers even when credentials came from command-line flags and
	// no file existed yet, in which case it points at the default save target.
	configPath string
	Stdout     io.Writer
	Stderr     io.Writer
	Ctx        context.Context
}

// ConfigPath returns the config file selected for this authenticated app.
// The path is also populated when credentials were supplied entirely through
// flags, so a caller can persist UI preferences beside the auth config.
func (a *App) ConfigPath() string {
	if a == nil {
		return ""
	}
	return a.configPath
}

type Options struct {
	ConfigPath string
	URL        string
	User       string
	Pass       string
	Hash       string
}

func (a *App) errf(format string, args ...any) {
	fmt.Fprintf(a.Stderr, "[ERROR] "+format+"\n", args...)
}

func newAppDefaults(app *App) {
	if app.Stdout == nil {
		app.Stdout = os.Stdout
	}
	if app.Stderr == nil {
		app.Stderr = os.Stderr
	}
	if app.Ctx == nil {
		app.Ctx = context.Background()
	}
}

func (a *App) context() context.Context {
	if a.Ctx != nil {
		return a.Ctx
	}
	return context.Background()
}
