//go:build !unix && !windows

package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
)

func configureDevelopmentDebugger(*exec.Cmd) {}

func snapshotDevelopmentDebugger(context.Context, int) ([]developmentDebugChild, error) {
	return nil, nil
}

func killDevelopmentDebugger(process *os.Process, _ []developmentDebugChild, running bool) error {
	if !running {
		return nil
	}
	err := process.Kill()
	if errors.Is(err, os.ErrProcessDone) {
		return nil
	}
	return err
}
