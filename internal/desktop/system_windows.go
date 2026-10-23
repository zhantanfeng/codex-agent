package desktop

import (
	"errors"
	"fmt"
	"os"
	"runtime"
	"sync"
	"sync/atomic"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

const startupKey = `Software\Microsoft\Windows\CurrentVersion\Run`
const startupValue = "DcodeDesktop"

func isAutostart() bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, startupKey, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	v, _, err := k.GetStringValue(startupValue)
	exe, _ := os.Executable()
	return err == nil && v == fmt.Sprintf("\"%s\" --hidden", exe)
}
func setAutostart(enabled bool) error {
	k, _, err := registry.CreateKey(registry.CURRENT_USER, startupKey, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	if !enabled {
		err = k.DeleteValue(startupValue)
		if errors.Is(err, registry.ErrNotExist) {
			return nil
		}
		return err
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	return k.SetStringValue(startupValue, fmt.Sprintf("\"%s\" --hidden", exe))
}

type powerChange struct {
	enabled bool
	result  chan error
}
type powerKeeper struct {
	mu       sync.RWMutex
	closed   bool
	requests chan powerChange
	done     chan struct{}
	active   atomic.Bool
}

func newPowerKeeper() *powerKeeper {
	p := &powerKeeper{requests: make(chan powerChange), done: make(chan struct{})}
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		defer close(p.done)
		set := windows.NewLazySystemDLL("kernel32.dll").NewProc("SetThreadExecutionState")
		defer set.Call(0x80000000)
		for r := range p.requests {
			flags := uintptr(0x80000000) // ES_CONTINUOUS; allow display sleep.
			if r.enabled {
				flags |= 1
			} // ES_SYSTEM_REQUIRED
			result, _, err := set.Call(flags)
			if result == 0 {
				r.result <- fmt.Errorf("设置保持唤醒失败：%w", err)
			} else {
				p.active.Store(r.enabled)
				r.result <- nil
			}
		}
	}()
	return p
}
func (p *powerKeeper) set(enabled bool) error {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.closed {
		return errors.New("桌面程序正在退出")
	}
	result := make(chan error, 1)
	p.requests <- powerChange{enabled, result}
	return <-result
}
func (p *powerKeeper) close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return
	}
	p.closed = true
	close(p.requests)
	<-p.done
	p.active.Store(false)
}
