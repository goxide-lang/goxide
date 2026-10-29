package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os/exec"
	"time"
)

type developmentDebugChild struct {
	pid      int
	identity string
}

// Keep Delve alive while discovering its children: macOS debugserver starts
// the debuggee in a different process group, even before launch completes.
func stopDevelopmentDebugger(command *exec.Cmd, connection net.Conn, sequence int, disconnected bool) error {
	snapshot := func() ([]developmentDebugChild, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		defer cancel()
		return snapshotDevelopmentDebugger(ctx, command.Process.Pid)
	}
	children, snapshotErr := snapshot()
	if connection != nil {
		defer connection.Close()
		if !disconnected {
			request, _ := json.Marshal(map[string]any{"seq": sequence, "type": "request", "command": "disconnect", "arguments": map[string]any{"terminateDebuggee": true}})
			_ = connection.SetWriteDeadline(time.Now().Add(250 * time.Millisecond))
			_, _ = fmt.Fprintf(connection, "Content-Length: %d\r\n\r\n%s", len(request), request)
		}
	}
	waited := make(chan error, 1)
	go func() { waited <- command.Wait() }()
	grace := time.Duration(0)
	if connection != nil {
		grace = 500 * time.Millisecond
	}
	stopped := false
	select {
	case <-waited:
		stopped = true
	case <-time.After(grace):
	}
	killErr := killDevelopmentDebugger(command.Process, children, !stopped)
	if !stopped {
		select {
		case <-waited:
		case <-time.After(1500 * time.Millisecond):
			return errors.Join(snapshotErr, killErr, fmt.Errorf("Delve did not exit after cancellation"))
		}
	}
	return errors.Join(snapshotErr, killErr)
}
