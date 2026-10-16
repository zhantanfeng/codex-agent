package agent

import (
	"crypto/sha256"
	"fmt"
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

func configureChildProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_NO_WINDOW}
}

func acquireRunLock(host string) (func(), error) {
	name, _ := windows.UTF16PtrFromString(fmt.Sprintf("Local\\DcodeAgent-%x", sha256.Sum256([]byte(host))))
	handle, err := windows.CreateMutex(nil, false, name)
	if err == windows.ERROR_ALREADY_EXISTS {
		if handle != 0 {
			windows.CloseHandle(handle)
		}
		return nil, ErrAlreadyRunning
	}
	if err != nil {
		return nil, err
	}
	return func() { windows.CloseHandle(handle) }, nil
}
