//go:build windows

package cmd

// killStaleTunnels is a no-op on Windows — lsof/ps/syscall.Kill are unavailable.
func killStaleTunnels(ports []int) {}
