package agent

import (
	"context"
	"os/exec"
	"strings"
	"time"
)

func runSecurity(input string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/usr/bin/security", args...)
	if input != "" {
		cmd.Stdin = strings.NewReader(input)
	}
	// Never include security's output in errors: it may contain secret data.
	return cmd.Output()
}

func protectSecret(plain []byte) ([]byte, error) {
	return protectKeychainSecret(plain, runSecurity)
}

func unprotectSecret(protected []byte) ([]byte, error) {
	return unprotectKeychainSecret(protected, runSecurity)
}
