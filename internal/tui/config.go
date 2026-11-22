//go:build !plan9

package tui

import (
	"context"
	"errors"
	"io"
	"net/url"
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/naterator/qbitctl/pkg/client"
)

// setupState is deliberately smaller than client.Setup. In particular, it
// never contains a password: the TUI can show whether one is saved without
// ever loading or rendering the secret.
type setupState struct {
	path        string
	url         string
	user        string
	hasPassword bool
	tuiDefault  bool
	ready       bool
}

func setupStateFromClient(setup client.Setup) setupState {
	return setupState{
		path:        setup.Path,
		url:         setup.URL,
		user:        setup.User,
		hasPassword: setup.HasPassword,
		tuiDefault:  setup.TUIDefault,
		ready:       setup.Ready(),
	}
}

func savedPasswordMetadata(setup client.Setup) client.Setup {
	if setup.Path == "" {
		return setup
	}
	saved, err := client.ReadSetup(client.Options{ConfigPath: setup.Path})
	if err == nil {
		// URL/user may be supplied by flags. Only replace the password metadata
		// from the config-only read so --pass is never presented as saved.
		setup.HasPassword = saved.HasPassword
	} else {
		setup.HasPassword = false
	}
	return setup
}

func validateConfigURL(value string) error {
	_, err := client.ValidateConfigURL(value)
	return err
}

func (m *model) toggleTUIDefault() tea.Cmd {
	next := !m.setup.tuiDefault
	if m.configPath == "" {
		// A caller without a resolved save path cannot persist the choice yet.
		m.setup.tuiDefault = next
		m.setNotice("TUI startup preference will be saved with configuration", false)
		return nil
	}
	enabled := next
	return m.startWork(configWork, "Saving startup preference", func(context.Context) workDone {
		err := client.SetTUIDefault(m.configPath, enabled)
		return workDone{setDefault: &enabled, err: err}
	})
}

func (m *model) toggleDataUnits() tea.Cmd {
	next := client.DataUnitsBytes
	if m.dataUnits == client.DataUnitsBytes {
		next = client.DataUnitsHuman
	}
	if m.configPath == "" {
		m.applyDataUnits(next)
		m.setNotice("Data units selected for this session", false)
		return nil
	}
	return m.startWork(configWork, "Saving data-unit preference", func(context.Context) workDone {
		err := client.SetDataUnits(m.configPath, next)
		return workDone{setDataUnits: &next, err: err}
	})
}

func (m *model) saveConfig(values client.Credentials, tuiDefault bool) tea.Cmd {
	if m.busy != "" {
		if m.dialog != nil {
			m.dialog.status = "Finish the current request first"
		}
		return nil
	}
	values.URL = strings.TrimSpace(values.URL)
	values.User = strings.TrimSpace(values.User)
	if err := validateConfigURL(values.URL); err != nil {
		if m.dialog != nil {
			m.dialog.status = err.Error()
		}
		return nil
	}
	if values.User == "" {
		if m.dialog != nil {
			m.dialog.status = "Username is required"
		}
		return nil
	}
	path := m.configPath
	if path == "" {
		path = m.setup.path
	}
	if path == "" {
		if m.dialog != nil {
			m.dialog.status = "No configuration path is available"
		}
		return nil
	}
	m.dialog = nil
	return m.startWork(configWork, "Testing connection and saving configuration", func(ctx context.Context) workDone {
		app, err := client.ConnectAndSaveSetup(ctx, path, values, tuiDefault)
		if err != nil {
			return workDone{err: err}
		}
		return workDone{
			app: app,
			setup: setupState{
				path:        path,
				url:         values.URL,
				user:        values.User,
				hasPassword: true,
				tuiDefault:  tuiDefault,
				ready:       true,
			},
		}
	})
}

func (m *model) resetRemoteState() {
	if m.searchPluginCancel != nil {
		m.searchPluginCancel()
	}
	m.searchPluginGeneration++
	m.allTorrents = nil
	m.queue = nil
	m.allResults = nil
	m.results = nil
	m.serverRows = nil
	m.queueIndex = 0
	m.resultIndex = 0
	m.marked = make(map[string]bool)
	m.query = ""
	m.filter = ""
	m.stateFilter = "all"
	m.resultFilter = resultFilter{}
	m.resultEditor = nil
	m.searchPlugin = defaultSearchPluginSelection
	m.savedSearchPlugin = defaultSearchPluginSelection
	m.searchPluginDefaultActive = true
	m.searchPluginSelectionGeneration++
	m.searchPlugins = nil
	m.searchPluginsAttempted = false
	m.searchPluginsLoading = false
	m.searchPluginCancel = nil
	m.searchPluginsStale = false
	m.searchPluginsError = ""
	m.dialog = nil
}

// setDefaultSearchPlugin applies the provider chosen in the explicit picker
// and persists it beside the qBittorrent credentials when a config path is
// available. Search-dialog edits intentionally do not call this method: they
// remain session-only overrides.
func (m *model) setDefaultSearchPlugin(selection string) error {
	selection = normalizeSavedSearchPlugin(selection)
	if strings.Contains(selection, "|") || strings.EqualFold(selection, "multi") {
		err := errors.New("default search provider must be one provider or enabled/all")
		m.setNotice("Default search provider: "+err.Error(), true)
		return err
	}

	path := strings.TrimSpace(m.configPath)
	if path == "" {
		path = strings.TrimSpace(m.preferencesPath)
	}
	var err error
	if path != "" {
		err = client.SetDefaultSearchPlugin(path, selection)
	}
	// Apply the in-memory choice even when persistence fails. The user can
	// continue this session, while the notice makes the persistence failure
	// visible and the next launch will retain the previous saved value.
	m.savedSearchPlugin = selection
	m.searchPlugin = selection
	m.searchPluginDefaultActive = true
	m.searchPluginSelectionGeneration++
	if err != nil {
		m.setNotice("Default search provider: "+err.Error(), true)
		return err
	}
	if path == "" {
		m.setNotice("Search provider selected for this session", false)
	} else {
		m.setNotice("Default search provider saved", false)
	}
	return nil
}

// RunConfigurable is the public TUI entry point used by the root command. It
// starts the workspace immediately when flags or an existing config are
// sufficient, then performs authentication as visible background work.
func RunConfigurable(ctx context.Context, opts client.Options, input io.Reader, output io.Writer) error {
	setup, setupErr := readSetupState(opts)
	if setupErr == nil && setup.ready {
		return runConfigurableConnecting(ctx, opts, setup, false, input, output)
	}
	if setupErr == nil && optionsHaveCredentials(opts) {
		return runConfigurableConnecting(ctx, opts, setup, true, input, output)
	}
	if setupErr != nil && optionsHaveCredentials(opts) {
		return runConfigurableConnecting(ctx, opts, setup, true, input, output)
	}
	if setupErr != nil {
		// configfile.Update cannot safely repair an object that could not be
		// decoded. Return the actual diagnostic rather than suggesting that the
		// Config page can overwrite a malformed file.
		return setupErr
	}
	return runConfigurableBootstrap(ctx, opts, setup, setupErr, input, output)
}

func runConfigurableConnecting(ctx context.Context, opts client.Options, setup setupState, explicit bool, input io.Reader, output io.Writer) error {
	sessionCtx, stop := context.WithCancel(ctx)
	m := newModel(sessionCtx, nil)
	m.page = queuePage
	m.setup = setup
	m.dataUnits = opts.DataUnits
	m.dataUnitsExplicit = opts.DataUnits != ""
	m.configPath = setup.path
	if m.configPath == "" {
		m.configPath = opts.ConfigPath
	}
	m.loadPreferences(m.configPath)
	m.connect = &opts
	m.connectExplicit = explicit
	m.connectTarget = connectionTarget(opts, setup)
	return runConfigurableSession(sessionCtx, stop, m, input, output)
}

func connectionTarget(opts client.Options, setup setupState) string {
	value := strings.TrimSpace(setup.url)
	if strings.TrimSpace(opts.URL) != "" {
		value = strings.TrimSpace(opts.URL)
	}
	parsed, err := url.Parse(value)
	if err == nil && parsed.Hostname() != "" {
		if parsed.Port() != "" {
			return parsed.Hostname() + ":" + parsed.Port()
		}
		return parsed.Hostname()
	}
	return "server"
}

func readSetupState(opts client.Options) (setupState, error) {
	setup, err := client.ReadSetup(opts)
	if err != nil {
		return setupState{path: opts.ConfigPath}, err
	}
	ready := setup.Ready()
	state := setupStateFromClient(savedPasswordMetadata(setup))
	state.ready = ready
	return state, nil
}

func optionsHaveCredentials(opts client.Options) bool {
	return strings.TrimSpace(opts.URL) != "" && strings.TrimSpace(opts.User) != "" && opts.Pass != ""
}

func parseTUIChoice(value string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "on", "true", "tui", "yes", "y":
		return true, nil
	case "0", "off", "false", "no", "n", "text":
		return false, nil
	default:
		return false, errors.New("TUI default must be yes or no")
	}
}

func runConfigurableApp(ctx context.Context, app *client.App, setup setupState, input io.Reader, output io.Writer) error {
	sessionCtx, stop := context.WithCancel(ctx)
	m := newModel(sessionCtx, &tuiClient{app: app})
	m.app, m.ownsApp = app, true
	m.setup = setup
	m.dataUnits = app.DataUnits
	m.applyDataUnits(m.dataUnits)
	m.configPath = setup.path
	if m.configPath == "" {
		m.configPath = app.ConfigPath()
	}
	m.loadPreferences(m.configPath)
	return runConfigurableSession(sessionCtx, stop, m, input, output)
}

func runConfigurableBootstrap(ctx context.Context, opts client.Options, setup setupState, setupErr error, input io.Reader, output io.Writer) error {
	sessionCtx, stop := context.WithCancel(ctx)
	m := newModel(sessionCtx, nil)
	m.page = configPage
	m.setup = setup
	m.dataUnits = opts.DataUnits
	m.dataUnitsExplicit = opts.DataUnits != ""
	m.configPath = setup.path
	if m.configPath == "" {
		m.configPath = opts.ConfigPath
	}
	m.loadPreferences(m.configPath)
	if setupErr != nil {
		m.setNotice("Saved credentials could not connect: "+setupErr.Error(), true)
	}
	m.openConfigDialog()
	if setupErr != nil && m.dialog != nil {
		m.dialog.status = "Connection failed: " + setupErr.Error()
	}
	return runConfigurableSession(sessionCtx, stop, m, input, output)
}

func runConfigurableSession(sessionCtx context.Context, stop context.CancelFunc, m *model, input io.Reader, output io.Writer) error {
	defer func() {
		stop()
		m.workers.Wait()
		m.cleanupPendingApps()
		if m.ownsApp && m.app != nil {
			m.app.Logout()
		}
	}()
	if input == nil {
		input = os.Stdin
	}
	if output == nil {
		output = os.Stdout
	}
	program := tea.NewProgram(m, tea.WithContext(sessionCtx), tea.WithoutSignalHandler(), tea.WithInput(input), tea.WithOutput(output))
	_, err := program.Run()
	if errors.Is(err, tea.ErrProgramKilled) && sessionCtx.Err() != nil {
		return nil
	}
	if m.exitErr != nil {
		return m.exitErr
	}
	return err
}
