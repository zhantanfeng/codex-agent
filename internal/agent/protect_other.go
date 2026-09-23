//go:build !windows

package agent

import "errors"

func protectSecret([]byte) ([]byte, error) {
	return nil, errors.New("the Windows Agent identity requires Windows DPAPI")
}

func unprotectSecret([]byte) ([]byte, error) {
	return nil, errors.New("the Windows Agent identity requires Windows DPAPI")
}
