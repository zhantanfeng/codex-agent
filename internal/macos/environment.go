package macos

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// Finder and launchd don't inherit the interactive terminal's PATH.
func PrepareEnvironment() {
	if runtime.GOOS != "darwin" {
		return
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	path := os.Getenv("PATH")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// Delimit the value so shell startup messages cannot become path entries.
	cmd := exec.CommandContext(ctx, "/bin/zsh", "-l", "-c", `printf '\0%s\0' "$PATH"`)
	cmd.WaitDelay = time.Second
	if output, err := cmd.Output(); err == nil {
		parts := strings.Split(string(output), "\x00")
		if len(parts) >= 3 && parts[len(parts)-2] != "" {
			path = parts[len(parts)-2] + ":" + path
		}
	}
	_ = os.Setenv("PATH", SearchPath(path, home))
}

func SearchPath(current, home string) string {
	entries := append(strings.Split(current, ":"),
		filepath.ToSlash(filepath.Join(home, ".local", "bin")),
		filepath.ToSlash(filepath.Join(home, ".npm-global", "bin")),
		filepath.ToSlash(filepath.Join(home, ".volta", "bin")),
		"/opt/homebrew/bin", "/usr/local/bin", "/usr/bin", "/bin", "/usr/sbin", "/sbin")
	seen := make(map[string]bool)
	var result []string
	for _, entry := range entries {
		if entry != "" && !seen[entry] {
			seen[entry] = true
			result = append(result, entry)
		}
	}
	return strings.Join(result, ":")
}
