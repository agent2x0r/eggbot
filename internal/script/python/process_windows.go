//go:build windows

package python

import "os/exec"

func configureProcess(cmd *exec.Cmd) {}

func killProcess(cmd *exec.Cmd) {
	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
}
