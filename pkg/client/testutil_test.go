package client

import (
	"net/http"
	"testing"

	"github.com/naterator/qbitctl/internal/qbtest"
)

type qbTestServer struct {
	t *testing.T
	*qbtest.Server
	Torrents     []TorrentInfo
	Trackers     []TrackerEntry
	TransferInfo *TransferInfo
}

func newQBTestServer(t *testing.T) *qbTestServer {
	t.Helper()
	return &qbTestServer{t: t, Server: qbtest.New(t)}
}

func (s *qbTestServer) creds() Credentials {
	creds := s.Creds()
	return Credentials{
		URL:  creds.URL,
		User: creds.User,
		Pass: creds.Pass,
	}
}

func (s *qbTestServer) newApp() *App {
	creds := s.creds()
	withQBServerClientFactory(s.t, s)
	app := &App{
		creds: creds,
	}
	newAppDefaults(app)
	app.client = newClient(creds, app.Stderr)
	return app
}

func withQBServerClientFactory(t *testing.T, s *qbTestServer) {
	t.Helper()
	prevFactory := httpClientFactory
	httpClientFactory = func(jar http.CookieJar) *http.Client {
		return &http.Client{
			Jar:       jar,
			Transport: qbtest.RoundTripFunc(func(req *http.Request) (*http.Response, error) { return s.roundTrip(req), nil }),
		}
	}
	t.Cleanup(func() {
		httpClientFactory = prevFactory
	})
}

func (s *qbTestServer) roundTrip(req *http.Request) *http.Response {
	s.Server.Torrents = s.Torrents
	s.Server.Trackers = s.Trackers
	if s.TransferInfo != nil {
		s.Server.TransferInfo = s.TransferInfo
	}
	resp := s.Server.RoundTrip(req)
	if torrents, ok := s.Server.Torrents.([]TorrentInfo); ok {
		s.Torrents = torrents
	}
	return resp
}
