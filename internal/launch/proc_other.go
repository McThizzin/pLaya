//go:build !unix

package launch

import (
	"os"
	"os/exec"
)

func configureProcess(cmd *exec.Cmd) {}

func interruptProcess(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	return cmd.Process.Signal(os.Interrupt)
}

func killProcess(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	return cmd.Process.Kill()
}
