package desktop

import (
	"encoding/xml"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"

	"codexremote/internal/macos"
)

func isAutostart() bool {
	path, err := macos.LaunchAgentPath(macos.DesktopLabel)
	if err != nil {
		return false
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	var job struct {
		Arguments []string `xml:"dict>array>string"`
	}
	if xml.Unmarshal(raw, &job) != nil || len(job.Arguments) != 2 {
		return false
	}
	executable, err := os.Executable()
	return err == nil && job.Arguments[0] == executable && job.Arguments[1] == "--hidden"
}

func setAutostart(enabled bool) error {
	if !enabled {
		return macos.RemoveLaunchAgent(macos.DesktopLabel)
	}
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	return (macos.LaunchAgent{Label: macos.DesktopLabel, Arguments: []string{filepath.Clean(executable), "--hidden"}, Path: os.Getenv("PATH")}).Install()
}

type powerKeeper struct {
	mu     sync.Mutex
	closed bool
	cmd    *exec.Cmd
	done   chan struct{}
	active atomic.Bool
}

func newPowerKeeper() *powerKeeper { return &powerKeeper{} }

func (p *powerKeeper) stopLocked() {
	if p.cmd != nil {
		_ = p.cmd.Process.Kill()
		<-p.done
		p.cmd = nil
	}
	p.active.Store(false)
}

func (p *powerKeeper) set(enabled bool) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return errors.New("桌面程序正在退出")
	}
	if !enabled {
		p.stopLocked()
		return nil
	}
	if p.cmd != nil {
		select {
		case <-p.done:
			p.cmd = nil
		default:
			return nil
		}
	}
	cmd := exec.Command("/usr/bin/caffeinate", "-i", "-w", strconv.Itoa(os.Getpid()))
	if err := cmd.Start(); err != nil {
		return err
	}
	done := make(chan struct{})
	p.cmd, p.done = cmd, done
	p.active.Store(true)
	go func() {
		_ = cmd.Wait()
		p.active.Store(false)
		close(done)
	}()
	return nil
}

func (p *powerKeeper) close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return
	}
	p.closed = true
	p.stopLocked()
}
