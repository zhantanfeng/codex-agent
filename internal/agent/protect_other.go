//go:build !windows && !darwin

package agent

import "errors"

func protectSecret([]byte) ([]byte, error) {
	return nil, errors.New("identity protection is supported on Windows and macOS")
}

func unprotectSecret([]byte) ([]byte, error) {
	return nil, errors.New("identity protection is supported on Windows and macOS")
}
