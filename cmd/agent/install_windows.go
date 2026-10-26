package main

import (
	"fmt"
	"os"
	"os/exec"
)

func installAutostart() error {
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	output, err := exec.Command("schtasks.exe", "/Create", "/SC", "ONLOGON", "/TN", "CodexRemoteAgent", "/TR", fmt.Sprintf("\"%s\" run", executable), "/F").CombinedOutput()
	if err != nil {
		return fmt.Errorf("install scheduled task: %w: %s", err, output)
	}
	fmt.Print(string(output))
	return nil
}

func uninstallAutostart() error {
	output, err := exec.Command("schtasks.exe", "/Delete", "/TN", "CodexRemoteAgent", "/F").CombinedOutput()
	if err != nil {
		return fmt.Errorf("remove scheduled task: %w: %s", err, output)
	}
	fmt.Print(string(output))
	return nil
}
