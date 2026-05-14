//go:build windows

package cli

import "os/exec"

func detach(cmd *exec.Cmd) {}
