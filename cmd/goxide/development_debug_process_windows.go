package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strconv"
	"time"
)

func configureDevelopmentDebugger(*exec.Cmd) {}

func snapshotDevelopmentDebugger(context.Context, int) ([]developmentDebugChild, error) {
	return nil, nil
}

func killDevelopmentDebugger(process *os.Process, _ []developmentDebugChild, running bool) error {
	if !running {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_ = exec.CommandContext(ctx, "taskkill", "/T", "/F", "/PID", strconv.Itoa(process.Pid)).Run()
	err := process.Kill()
	if errors.Is(err, os.ErrProcessDone) {
		return nil
	}
	return err
}
