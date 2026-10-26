package desktop

// macOS uses its Dock and application menu instead of a second Cocoa event
// loop for a tray library. The menu always provides a way to reopen the window.
func (a *App) startTray() { a.trayReady.Store(true) }
func (a *App) stopTray()  { a.trayReady.Store(false) }
