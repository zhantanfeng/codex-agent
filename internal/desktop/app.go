//go:build windows || darwin

package desktop

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"codexremote/internal/agent"
	"codexremote/internal/protocol"
	qrcode "github.com/skip2/go-qrcode"
	wr "github.com/wailsapp/wails/v2/pkg/runtime"
)

type Preferences struct {
	KeepAwake bool `json:"keepAwake"`
}
type State struct {
	agent.Status
	Platform    string   `json:"platform"`
	LogPath     string   `json:"logPath"`
	Configured  bool     `json:"configured"`
	Running     bool     `json:"running"`
	Stopping    bool     `json:"stopping"`
	HostID      string   `json:"hostId"`
	RelayURL    string   `json:"relayUrl"`
	Devices     []string `json:"devices"`
	Projects    int      `json:"projects"`
	Error       string   `json:"error"`
	KeepAwake   bool     `json:"keepAwake"`
	AwakeActive bool     `json:"awakeActive"`
	Autostart   bool     `json:"autostart"`
	Logs        []string `json:"logs"`
}

type App struct {
	ctx       context.Context
	store     *agent.Store
	mu        sync.Mutex
	op        sync.Mutex
	runtime   *agent.Runtime
	cancel    context.CancelFunc
	done      chan struct{}
	stopping  bool
	lastError string
	prefs     Preferences
	logs      *logBuffer
	power     *powerKeeper
	quitting  atomic.Bool
	trayReady atomic.Bool
}

func New() (*App, error) {
	store, err := agent.DefaultStore()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(store.Dir, 0700); err != nil {
		return nil, err
	}
	logs, err := newLogBuffer(filepath.Join(store.Dir, "desktop.log"))
	if err != nil {
		return nil, err
	}
	a := &App{store: store, logs: logs, power: newPowerKeeper()}
	raw, err := os.ReadFile(filepath.Join(store.Dir, "desktop.json"))
	if err == nil {
		if err := json.Unmarshal(raw, &a.prefs); err != nil {
			a.recordError(fmt.Errorf("读取桌面设置：%w", err))
		}
	}
	return a, nil
}

func (a *App) Startup(ctx context.Context) {
	a.ctx = ctx
	a.startTray()
	if _, err := a.store.LoadConfig(); err == nil {
		_ = a.Start()
	}
}

func (a *App) BeforeClose(ctx context.Context) bool {
	if a.quitting.Load() || !a.trayReady.Load() {
		return false
	}
	wr.WindowHide(ctx)
	return true
}

func (a *App) Shutdown(ctx context.Context) {
	_ = a.Stop()
	a.power.close()
	a.stopTray()
	a.logs.close()
}

func (a *App) Show() {
	if a.ctx != nil {
		wr.WindowShow(a.ctx)
		wr.WindowUnminimise(a.ctx)
	}
}
func (a *App) Quit() { a.quitting.Store(true); wr.Quit(a.ctx) }

func (a *App) GetState() State {
	a.mu.Lock()
	s := State{Platform: runtime.GOOS, LogPath: filepath.Join(a.store.Dir, "desktop.log"), Running: a.cancel != nil, Stopping: a.stopping, Error: a.lastError, KeepAwake: a.prefs.KeepAwake, Devices: []string{}}
	r := a.runtime
	a.mu.Unlock()
	if r != nil {
		s.Status = r.Status()
	}
	if config, err := a.store.LoadConfig(); err == nil {
		s.Configured, s.HostID, s.RelayURL, s.Projects = true, config.HostID, config.RelayURL, len(config.Projects)
		for id := range config.Clients {
			s.Devices = append(s.Devices, id)
		}
		sort.Strings(s.Devices)
		if config.Pairing == nil || time.Now().After(s.LinkExpires) {
			s.LinkCode = ""
		}
	}
	s.Autostart = isAutostart()
	s.AwakeActive = a.power.active.Load()
	s.Logs = a.logs.snapshot()
	return s
}

func (a *App) Configure(relayURL string) error {
	a.op.Lock()
	defer a.op.Unlock()
	relayURL = strings.TrimSpace(relayURL)
	u, err := url.Parse(relayURL)
	if err != nil || (u.Scheme != "ws" && u.Scheme != "wss") || u.Hostname() == "" || u.Fragment != "" {
		return errors.New("请输入完整的 ws:// 或 wss:// 服务器地址，例如 ws://43.155.135.230:8080/ws")
	}
	config, err := a.store.LoadConfig()
	if errors.Is(err, os.ErrNotExist) {
		_, _, err = a.store.Initialize(relayURL)
		if err != nil {
			a.recordError(err)
		}
		return err
	}
	if err != nil {
		return err
	}
	if config.RelayURL == relayURL {
		return nil
	}
	a.mu.Lock()
	wasRunning := a.cancel != nil
	a.mu.Unlock()
	if err := a.stop(); err != nil {
		return err
	}
	// Read again after shutdown so the final pairing and project updates are kept.
	config, err = a.store.LoadConfig()
	if err == nil {
		config.RelayURL = relayURL
		config.Pairing = nil
		config.Pending = map[string]agent.PendingClient{}
		err = a.store.Save(config)
	}
	if err != nil {
		if wasRunning {
			if restartErr := a.start(); restartErr != nil {
				err = errors.Join(err, restartErr)
			}
		}
		return err
	}
	a.mu.Lock()
	a.lastError = ""
	a.mu.Unlock()
	if wasRunning {
		return a.start()
	}
	return nil
}

func (a *App) Start() error {
	a.op.Lock()
	defer a.op.Unlock()
	return a.start()
}

// start and stop are called with op held to serialize configuration and lifecycle changes.
func (a *App) start() error {
	a.mu.Lock()
	if a.cancel != nil {
		a.mu.Unlock()
		return nil
	}
	a.mu.Unlock()
	if _, _, err := a.store.Load(); err != nil {
		a.recordError(err)
		return err
	}
	ctx, cancel := context.WithCancel(a.ctx)
	done := make(chan struct{})
	a.mu.Lock()
	a.cancel, a.done, a.lastError = cancel, done, ""
	awake := a.prefs.KeepAwake
	a.mu.Unlock()
	if err := a.power.set(awake); err != nil {
		a.recordError(err)
	}
	go a.supervise(ctx, done)
	return nil
}

func (a *App) supervise(ctx context.Context, done chan struct{}) {
	defer func() {
		_ = a.power.set(false)
		a.mu.Lock()
		a.runtime, a.cancel, a.stopping = nil, nil, false
		a.mu.Unlock()
		close(done)
	}()
	log := slog.New(slog.NewJSONHandler(a.logs, nil))
	for ctx.Err() == nil {
		config, private, err := a.store.Load()
		var r *agent.Runtime
		if err == nil {
			r, err = agent.NewRuntime(a.store, config, private, log)
		}
		if err == nil {
			a.mu.Lock()
			a.runtime = r
			a.mu.Unlock()
			runCtx, cancel := context.WithCancel(ctx)
			result := make(chan error, 1)
			go func() { result <- r.Run(runCtx) }()
			ticker := time.NewTicker(time.Second)
			wasReady := false
			wasOnline := false
		wait:
			for {
				select {
				case err = <-result:
					break wait
				case <-ticker.C:
					status := r.Status()
					if status.CodexOnline && status.RelayOnline && !wasOnline {
						a.mu.Lock()
						a.lastError = ""
						a.mu.Unlock()
						wasOnline = true
					}
					if status.CodexOnline {
						wasReady = true
					}
					if wasReady && !status.CodexOnline {
						cancel()
						<-result
						err = errors.New("Codex 后台已退出，正在自动重启")
						break wait
					}
				}
			}
			ticker.Stop()
			cancel()
		}
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			a.recordError(err)
		}
		if errors.Is(err, agent.ErrAlreadyRunning) {
			return
		}
		a.mu.Lock()
		a.runtime = nil
		a.mu.Unlock()
		select {
		case <-ctx.Done():
			return
		case <-time.After(5 * time.Second):
		}
	}
}

func (a *App) Stop() error {
	a.op.Lock()
	defer a.op.Unlock()
	return a.stop()
}

func (a *App) stop() error {
	a.mu.Lock()
	cancel, done := a.cancel, a.done
	if cancel != nil {
		a.stopping = true
	}
	a.mu.Unlock()
	if cancel == nil {
		return nil
	}
	cancel()
	select {
	case <-done:
		return nil
	case <-time.After(10 * time.Second):
		return errors.New("后台仍在停止，请稍候再试")
	}
}

func (a *App) Restart() error {
	a.op.Lock()
	defer a.op.Unlock()
	if err := a.stop(); err != nil {
		return err
	}
	return a.start()
}

func (a *App) RefreshPairing() error {
	a.mu.Lock()
	r := a.runtime
	stopping := a.stopping
	a.mu.Unlock()
	if r == nil || stopping {
		return errors.New("请先启动电脑后台")
	}
	ctx, cancel := context.WithTimeout(a.ctx, 10*time.Second)
	defer cancel()
	if err := r.RefreshLinkPairing(ctx); err != nil {
		return err
	}
	return nil
}

// QR uses the existing signed identity and bootstrap token. It is returned
// only as a PNG; no private key is ever sent to the renderer.
func (a *App) PairingQR() (string, error) {
	config, private, err := a.store.Load()
	if err != nil {
		return "", err
	}
	if config.Pairing == nil || !time.Now().Before(config.Pairing.ExpiresAt) {
		return "", errors.New("配对码已过期，请刷新配对码")
	}
	signer, err := protocol.NewSigner(config.HostID, private)
	if err != nil {
		return "", err
	}
	payload, _ := json.Marshal(map[string]any{"version": 1, "relayUrl": config.RelayURL, "hostId": config.HostID, "hostPublicKey": signer.PublicKeyBase64(), "code": config.Pairing.Code})
	image, err := qrcode.Encode("codexremote://pair?data="+base64.RawURLEncoding.EncodeToString(payload), qrcode.Medium, 256)
	if err != nil {
		return "", err
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(image), nil
}

func (a *App) RevokeDevice(id string) error {
	a.op.Lock()
	defer a.op.Unlock()
	a.mu.Lock()
	r := a.runtime
	a.mu.Unlock()
	if r != nil {
		return r.RevokeClient(id)
	}
	config, err := a.store.LoadConfig()
	if err != nil {
		return err
	}
	delete(config.Clients, id)
	return a.store.Save(config)
}

func (a *App) SetKeepAwake(enabled bool) error {
	a.op.Lock()
	defer a.op.Unlock()
	a.mu.Lock()
	running := a.cancel != nil
	old := a.prefs
	a.mu.Unlock()
	if err := a.power.set(enabled && running); err != nil {
		return err
	}
	prefs := Preferences{KeepAwake: enabled}
	raw, _ := json.Marshal(prefs)
	path := filepath.Join(a.store.Dir, "desktop.json")
	err := os.WriteFile(path+".tmp", raw, 0600)
	if err == nil {
		err = os.Rename(path+".tmp", path)
	}
	if err != nil {
		_ = a.power.set(old.KeepAwake && running)
		return err
	}
	a.mu.Lock()
	a.prefs = prefs
	a.mu.Unlock()
	return nil
}

func (a *App) SetAutostart(enabled bool) error { return setAutostart(enabled) }
func (a *App) recordError(err error) {
	a.mu.Lock()
	a.lastError = err.Error()
	a.mu.Unlock()
	slog.New(slog.NewJSONHandler(a.logs, nil)).Error("desktop background", "error", err)
}
