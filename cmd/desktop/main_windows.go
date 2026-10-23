package main

import (
	"embed"
	"io/fs"
	"os"
	"path/filepath"
	"unsafe"

	"codexremote/internal/desktop"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
	win "golang.org/x/sys/windows"
)

//go:embed frontend/*
var assets embed.FS

func main() {
	desktop.TrayPNG, _ = assets.ReadFile("frontend/logo.png")
	app, err := desktop.New()
	if err != nil {
		showError(err)
		return
	}
	hidden := len(os.Args) > 1 && os.Args[1] == "--hidden"
	dataDir, _ := os.UserCacheDir()
	frontend, _ := fs.Sub(assets, "frontend")
	err = wails.Run(&options.App{
		Title: "dcode", Width: 1060, Height: 790, MinWidth: 840, MinHeight: 650,
		StartHidden:      hidden,
		BackgroundColour: options.NewRGB(246, 247, 244),
		AssetServer:      &assetserver.Options{Assets: frontend},
		OnStartup:        app.Startup, OnBeforeClose: app.BeforeClose, OnShutdown: app.Shutdown,
		Bind:               []interface{}{app},
		SingleInstanceLock: &options.SingleInstanceLock{UniqueId: "3b189975-c789-4c02-bd18-909e9f0499f1", OnSecondInstanceLaunch: func(options.SecondInstanceData) { app.Show() }},
		Windows:            &windows.Options{Theme: windows.Light, WebviewUserDataPath: filepath.Join(dataDir, "DcodeDesktop", "WebView2")},
	})
	if err != nil {
		showError(err)
	}
}

func showError(err error) {
	message, _ := win.UTF16PtrFromString("dcode 启动失败：" + err.Error())
	title, _ := win.UTF16PtrFromString("dcode")
	win.NewLazySystemDLL("user32.dll").NewProc("MessageBoxW").Call(0, uintptr(unsafe.Pointer(message)), uintptr(unsafe.Pointer(title)), 0x10)
}
