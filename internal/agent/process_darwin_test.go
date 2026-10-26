package agent

import (
	"errors"
	"testing"
)

func TestMacRunLockExcludesDuplicateAgentAndReleases(t *testing.T) {
	host := t.Name()
	release, err := acquireRunLock(host)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if duplicateRelease, err := acquireRunLock(host); !errors.Is(err, ErrAlreadyRunning) {
		if duplicateRelease != nil {
			duplicateRelease()
		}
		t.Fatalf("duplicate Agent acquired lock: %v", err)
	}
	release()
	releaseAgain, err := acquireRunLock(host)
	if err != nil {
		t.Fatal(err)
	}
	releaseAgain()
}
