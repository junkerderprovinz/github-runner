package runner

import (
	"os/exec"
	"strconv"
)

// The image runs on Linux. These let the service run on Windows during
// development, where the runner's process tree goes down at once.
func ownGroup(*exec.Cmd) {}

func terminate(cmd *exec.Cmd) error {
	return kill(cmd)
}

func kill(cmd *exec.Cmd) error {
	return exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(cmd.Process.Pid)).Run()
}
