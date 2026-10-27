//go:build unix

package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"
)

func configureDevelopmentDebugger(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

type developmentDebugProcessRow struct {
	developmentDebugChild
	parent int
}

func developmentDebugProcesses(ctx context.Context) ([]developmentDebugProcessRow, error) {
	command := exec.CommandContext(ctx, "ps", "-ax", "-o", "pid=,ppid=,lstart=,comm=")
	command.WaitDelay = 100 * time.Millisecond
	data, err := command.Output()
	if err != nil {
		return nil, fmt.Errorf("inspect Delve child processes: %w", err)
	}
	var rows []developmentDebugProcessRow
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 8 {
			continue
		}
		pid, pidErr := strconv.Atoi(fields[0])
		parent, parentErr := strconv.Atoi(fields[1])
		if pidErr == nil && parentErr == nil {
			rows = append(rows, developmentDebugProcessRow{developmentDebugChild{pid: pid, identity: strings.Join(fields[2:], " ")}, parent})
		}
	}
	return rows, nil
}

func snapshotDevelopmentDebugger(ctx context.Context, pid int) ([]developmentDebugChild, error) {
	rows, err := developmentDebugProcesses(ctx)
	if err != nil {
		return nil, err
	}
	owned := map[int]bool{pid: true}
	var children []developmentDebugChild
	for _, row := range rows {
		if row.pid == pid {
			children = append(children, row.developmentDebugChild)
		}
	}
	return append(children, developmentDebugDescendants(rows, owned)...), nil
}

func developmentDebugDescendants(rows []developmentDebugProcessRow, owned map[int]bool) []developmentDebugChild {
	var children []developmentDebugChild
	for added := true; added; {
		added = false
		for _, row := range rows {
			if owned[row.parent] && !owned[row.pid] {
				owned[row.pid], added = true, true
				children = append(children, row.developmentDebugChild)
			}
		}
	}
	return children
}

func killDevelopmentDebugger(process *os.Process, children []developmentDebugChild, running bool) error {
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	rows, err := developmentDebugProcesses(ctx)
	current := map[int]string{}
	for _, row := range rows {
		current[row.pid] = row.identity
	}
	owned := map[int]bool{}
	for _, child := range children {
		if current[child.pid] == child.identity {
			owned[child.pid] = true
		}
	}
	// Delve may already have exited. Its still-identifiable children can
	// nevertheless reveal targets they started during the disconnect grace.
	children = append(children, developmentDebugDescendants(rows, owned)...)
	seen := map[int]bool{}
	for i := len(children) - 1; i >= 0; i-- {
		child := children[i]
		if child.pid == process.Pid || seen[child.pid] || current[child.pid] != child.identity {
			continue
		}
		seen[child.pid] = true
		if killErr := syscall.Kill(child.pid, syscall.SIGKILL); killErr != nil && killErr != syscall.ESRCH {
			err = errors.Join(err, killErr)
		}
	}
	if running {
		// The group was created only for this Delve. Individually tracked
		// descendants above also include targets that created their own group.
		if owned[process.Pid] {
			if killErr := syscall.Kill(-process.Pid, syscall.SIGKILL); killErr != nil && killErr != syscall.ESRCH {
				err = errors.Join(err, killErr)
			}
		}
		if killErr := process.Kill(); killErr != nil && !errors.Is(killErr, os.ErrProcessDone) {
			err = errors.Join(err, killErr)
		}
	}
	return err
}
