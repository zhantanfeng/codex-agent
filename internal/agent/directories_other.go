//go:build !windows

package agent

func filesystemRoots() ([]directoryEntry, error) {
	return []directoryEntry{{Name: "/", Path: "/"}}, nil
}
