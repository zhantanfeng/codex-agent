package agent

import "golang.org/x/sys/windows"

func filesystemRoots() ([]directoryEntry, error) {
	mask, err := windows.GetLogicalDrives()
	if err != nil {
		return nil, err
	}
	entries := []directoryEntry{}
	for letter := 0; letter < 26; letter++ {
		if mask&(1<<letter) != 0 {
			path := string(rune('A'+letter)) + `:\`
			entries = append(entries, directoryEntry{Name: path, Path: path})
		}
	}
	return entries, nil
}
