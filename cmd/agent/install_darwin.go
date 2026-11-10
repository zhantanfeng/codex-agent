package main

import (
	"fmt"
	"os"
	"path/filepath"

	"codexremote/internal/agent"
	"codexremote/internal/macos"
)

func installAutostart() error {
	store, err := agent.DefaultStore()
	if err != nil {
		return err
	}
	if _, _, err := store.Load(); err != nil {
		return fmt.Errorf("initialize the Agent before installing login startup: %w", err)
	}
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	executable, err = filepath.EvalSymlinks(executable)
	if err != nil {
		return err
	}
	if err := (macos.LaunchAgent{Label: macos.AgentLabel, Arguments: []string{executable, "run"}, Path: os.Getenv("PATH"), LogPath: filepath.Join(store.Dir, "agent.log")}).Install(); err != nil {
		return err
	}
	fmt.Println("Registered for the next macOS login. Keep this executable at its current location.")
	return nil
}

func uninstallAutostart() error {
	if err := macos.RemoveLaunchAgent(macos.AgentLabel); err != nil {
		return err
	}
	fmt.Println("Login startup removed. Any currently running Agent stays running until stopped.")
	return nil
}
