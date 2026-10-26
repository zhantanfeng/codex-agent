package main

import (
	"embed"
	"fmt"
	"io/fs"
	"os"
	"os/exec"

	"codexremote/internal/desktop"
	"codexremote/internal/macos"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/menu"
	"github.com/wailsapp/wails/v2/pkg/menu/keys"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/mac"
)

//go:embed frontend/*
var assets embed.FS

func main() {
	macos.PrepareEnvironment()
	app, err := desktop.New()
	if err != nil {
		showError(err)
		return
	}
	appMenu := menu.NewMenu()
	dcodeMenu := appMenu.AddSubmenu("dcode")
	dcodeMenu.AddText("显示主窗口", keys.CmdOrCtrl("o"), func(*menu.CallbackData) { app.Show() })
	dcodeMenu.AddSeparator()
	dcodeMenu.AddText("退出 dcode", keys.CmdOrCtrl("q"), func(*menu.CallbackData) { app.Quit() })
	appMenu.Append(menu.EditMenu())
	frontend, _ := fs.Sub(assets, "frontend")
	err = wails.Run(&options.App{
		Title: "dcode", Width: 1060, Height: 790, MinWidth: 840, MinHeight: 650,
		StartHidden:      len(os.Args) > 1 && os.Args[1] == "--hidden",
		BackgroundColour: options.NewRGB(246, 247, 244),
		AssetServer:      &assetserver.Options{Assets: frontend},
		Menu:             appMenu,
		OnStartup:        app.Startup, OnBeforeClose: app.BeforeClose, OnShutdown: app.Shutdown,
		Bind:               []interface{}{app},
		SingleInstanceLock: &options.SingleInstanceLock{UniqueId: "3b189975-c789-4c02-bd18-909e9f0499f1", OnSecondInstanceLaunch: func(options.SecondInstanceData) { app.Show() }},
		Mac:                &mac.Options{TitleBar: mac.TitleBarDefault()},
	})
	if err != nil {
		showError(err)
	}
}

func showError(err error) {
	fmt.Fprintln(os.Stderr, "dcode:", err)
	// Pass error text as an argument, never interpolate it into AppleScript.
	_ = exec.Command("/usr/bin/osascript", "-e", `on run argv
display alert "dcode 启动失败" message (item 1 of argv) as critical
end run`, err.Error()).Run()
}
