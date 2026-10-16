package desktop

import (
	"bytes"
	"encoding/binary"
	"runtime"

	"github.com/energye/systray"
)

var TrayPNG []byte // Supplied by the desktop entrypoint from the existing app logo.

func (a *App) startTray() {
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		systray.Run(func() {
			if len(TrayPNG) > 0 {
				systray.SetIcon(pngIcon(TrayPNG))
			}
			systray.SetTooltip("dcode · 手机远程连接电脑")
			systray.SetOnClick(func(systray.IMenu) { a.Show() })
			systray.SetOnDClick(func(systray.IMenu) { a.Show() })
			systray.AddMenuItem("打开 dcode", "显示主界面").Click(a.Show)
			systray.AddSeparator()
			systray.AddMenuItem("启动后台", "连接手机与 Codex").Click(func() {
				go func() {
					if err := a.Start(); err != nil {
						a.recordError(err)
					}
				}()
			})
			systray.AddMenuItem("停止后台", "停止后手机将无法连接").Click(func() {
				go func() {
					if err := a.Stop(); err != nil {
						a.recordError(err)
					}
				}()
			})
			systray.AddSeparator()
			systray.AddMenuItem("退出 dcode", "停止后台并退出软件").Click(a.Quit)
			a.trayReady.Store(true)
		}, func() { a.trayReady.Store(false) })
	}()
}
func (a *App) stopTray() {
	if a.trayReady.Load() {
		systray.Quit()
	}
}

func pngIcon(png []byte) []byte {
	var b bytes.Buffer
	// ICO header and a single PNG image (the existing mdpi logo is 48x48).
	for _, v := range []uint16{0, 1, 1} {
		_ = binary.Write(&b, binary.LittleEndian, v)
	}
	b.Write([]byte{48, 48, 0, 0})
	for _, v := range []uint16{1, 32} {
		_ = binary.Write(&b, binary.LittleEndian, v)
	}
	_ = binary.Write(&b, binary.LittleEndian, uint32(len(png)))
	_ = binary.Write(&b, binary.LittleEndian, uint32(22))
	b.Write(png)
	return b.Bytes()
}
