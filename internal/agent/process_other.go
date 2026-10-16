//go:build !windows

package agent

import "os/exec"

func configureChildProcess(cmd *exec.Cmd)        {}
func acquireRunLock(host string) (func(), error) { return func() {}, nil }
