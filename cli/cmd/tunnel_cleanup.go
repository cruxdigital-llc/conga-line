//go:build !windows

package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
)

// killStaleTunnels finds and kills any conga processes listening on the given
// ports (i.e. leftover `conga connect` tunnels). Skips the current process.
func killStaleTunnels(ports []int) {
	if len(ports) == 0 {
		return
	}

	myPID := os.Getpid()

	for _, port := range ports {
		// Use lsof to find processes listening on the port
		out, err := exec.Command("lsof", "-ti", fmt.Sprintf("tcp:%d", port), "-sTCP:LISTEN").Output()
		if err != nil {
			continue
		}

		for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			pid, err := strconv.Atoi(strings.TrimSpace(line))
			if err != nil || pid == myPID {
				continue
			}

			// Verify it's a conga process before killing
			cmdOut, err := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "comm=").Output()
			if err != nil {
				continue
			}
			if !strings.Contains(strings.TrimSpace(string(cmdOut)), "conga") {
				continue
			}

			fmt.Printf("Closing stale tunnel (PID %d on port %d)...\n", pid, port)
			syscall.Kill(pid, syscall.SIGTERM)
		}
	}
}
