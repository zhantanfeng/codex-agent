//go:build !windows && !darwin

package main

import "errors"

func installAutostart() error   { return errors.New("login startup is supported on Windows and macOS") }
func uninstallAutostart() error { return errors.New("login startup is supported on Windows and macOS") }
