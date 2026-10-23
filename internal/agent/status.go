package agent

import (
	"context"
	"errors"
	"time"
)

var ErrAlreadyRunning = errors.New("电脑后台已在运行，请先退出另一个 dcode 桌面版或命令行 run 进程，再点击启动")

// Status contains display information only; identity keys and bootstrap tokens
// must never be exposed to a desktop renderer.
type Status struct {
	CodexOnline bool      `json:"codexOnline"`
	RelayOnline bool      `json:"relayOnline"`
	LinkCode    string    `json:"linkCode"`
	LinkExpires time.Time `json:"linkExpires"`
	LastPhone   time.Time `json:"lastPhone"`
}

func (r *Runtime) Status() Status {
	r.statusMu.RLock()
	s := Status{LinkCode: r.linkCode, LinkExpires: r.linkExpires, LastPhone: r.lastPhone}
	r.statusMu.RUnlock()
	config := r.currentConfig()
	if config.Pairing == nil || !config.Pairing.AutoApprove || !time.Now().Before(s.LinkExpires) {
		s.LinkCode = ""
	}
	r.codexMu.RLock()
	s.CodexOnline = r.codex != nil && r.codex.alive.Load()
	r.codexMu.RUnlock()
	r.connMu.Lock()
	s.RelayOnline = r.conn != nil
	r.connMu.Unlock()
	return s
}

// RefreshLinkPairing does not restart Codex or interrupt a running turn.
func (r *Runtime) RefreshLinkPairing(ctx context.Context) error {
	r.commandMu.Lock()
	defer r.commandMu.Unlock()
	if !r.Status().RelayOnline {
		return errors.New("尚未连接中转服务器，请连接后重试")
	}
	if err := r.prepareLinkPairing(); err != nil {
		return err
	}
	return r.registerLinkPairing(ctx)
}

func (r *Runtime) RevokeClient(id string) error {
	r.commandMu.Lock()
	defer r.commandMu.Unlock()
	config, err := r.store.LoadConfig()
	if err != nil {
		return err
	}
	if _, ok := config.Clients[id]; !ok {
		return errors.New("设备不存在或已移除")
	}
	delete(config.Clients, id)
	if err := r.store.Save(config); err != nil {
		return err
	}
	r.setConfig(config)
	return nil
}
