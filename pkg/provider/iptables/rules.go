// Package iptables provides shared iptables command generation and orchestration
// for egress enforcement. Providers call the Cmd functions to get shell command
// strings and the orchestration functions (AddRules, RemoveRules, CheckRules) to
// execute them via a provider-supplied callback.
package iptables

import (
	"fmt"
	"net"
)

// AddRulesCmd returns a shell command that idempotently inserts egress DROP rules
// into DOCKER-USER for the given container IP. Rules are inserted in reverse order
// (iptables -I pushes to top) so the final chain order is:
//
//  1. ESTABLISHED,RELATED → RETURN (allow response traffic)
//  2. dst=subnet → RETURN (allow proxy + Docker DNS)
//  3. DROP (block everything else from this source)
//
// Returns an error if containerIP or subnetCIDR are not well-formed.
func AddRulesCmd(containerIP, subnetCIDR string) (string, error) {
	if err := validateIP(containerIP); err != nil {
		return "", fmt.Errorf("invalid container IP: %w", err)
	}
	if err := validateCIDR(subnetCIDR); err != nil {
		return "", fmt.Errorf("invalid subnet CIDR: %w", err)
	}
	return fmt.Sprintf(
		"iptables -C DOCKER-USER -s %s -j DROP 2>/dev/null || iptables -I DOCKER-USER -s %s -j DROP; "+
			"iptables -C DOCKER-USER -s %s -d %s -j RETURN 2>/dev/null || iptables -I DOCKER-USER -s %s -d %s -j RETURN; "+
			"iptables -C DOCKER-USER -s %s -m conntrack --ctstate ESTABLISHED,RELATED -j RETURN 2>/dev/null || iptables -I DOCKER-USER -s %s -m conntrack --ctstate ESTABLISHED,RELATED -j RETURN",
		containerIP, containerIP,
		containerIP, subnetCIDR, containerIP, subnetCIDR,
		containerIP, containerIP), nil
}

// RemoveRulesCmd returns a shell command that removes egress iptables rules
// for the given container IP. Idempotent — each deletion is wrapped with || true.
// Returns ("", nil) if containerIP is empty (no-op).
// Returns an error if containerIP or subnetCIDR are not well-formed.
func RemoveRulesCmd(containerIP, subnetCIDR string) (string, error) {
	if containerIP == "" {
		return "", nil
	}
	if err := validateIP(containerIP); err != nil {
		return "", fmt.Errorf("invalid container IP: %w", err)
	}
	if err := validateCIDR(subnetCIDR); err != nil {
		return "", fmt.Errorf("invalid subnet CIDR: %w", err)
	}
	return fmt.Sprintf(
		"iptables -D DOCKER-USER -s %s -m conntrack --ctstate ESTABLISHED,RELATED -j RETURN 2>/dev/null || true; "+
			"iptables -D DOCKER-USER -s %s -d %s -j RETURN 2>/dev/null || true; "+
			"iptables -D DOCKER-USER -s %s -j DROP 2>/dev/null || true",
		containerIP,
		containerIP, subnetCIDR,
		containerIP), nil
}

// CheckRulesCmd returns a shell command that checks whether all three egress rules
// exist for the given container IP. Exits 0 only if all rules are present.
// Returns an error if containerIP or subnetCIDR are not well-formed.
func CheckRulesCmd(containerIP, subnetCIDR string) (string, error) {
	if err := validateIP(containerIP); err != nil {
		return "", fmt.Errorf("invalid container IP: %w", err)
	}
	if err := validateCIDR(subnetCIDR); err != nil {
		return "", fmt.Errorf("invalid subnet CIDR: %w", err)
	}
	return fmt.Sprintf(
		"iptables -C DOCKER-USER -s %s -m conntrack --ctstate ESTABLISHED,RELATED -j RETURN 2>/dev/null && "+
			"iptables -C DOCKER-USER -s %s -d %s -j RETURN 2>/dev/null && "+
			"iptables -C DOCKER-USER -s %s -j DROP 2>/dev/null",
		containerIP,
		containerIP, subnetCIDR,
		containerIP), nil
}

// ExecFunc executes a shell command string. Used by orchestration functions
// so providers can plug in their own execution mechanism (local exec, SSH, nsenter).
type ExecFunc func(cmd string) error

// AddRules generates and executes iptables ADD commands via the provided callback.
func AddRules(containerIP, subnetCIDR string, run ExecFunc) error {
	cmds, err := AddRulesCmd(containerIP, subnetCIDR)
	if err != nil {
		return err
	}
	return run(cmds)
}

// RemoveRules generates and executes iptables REMOVE commands via the provided callback.
// No-op if containerIP is empty. Errors from iptables are ignored (idempotent removal).
func RemoveRules(containerIP, subnetCIDR string, run ExecFunc) {
	cmds, err := RemoveRulesCmd(containerIP, subnetCIDR)
	if err != nil || cmds == "" {
		return
	}
	run(cmds) //nolint:errcheck // removal is best-effort
}

// CheckRules generates and executes iptables CHECK commands via the provided callback.
// Returns true only if all three rules are present.
func CheckRules(containerIP, subnetCIDR string, run ExecFunc) bool {
	cmds, err := CheckRulesCmd(containerIP, subnetCIDR)
	if err != nil {
		return false
	}
	return run(cmds) == nil
}

func validateIP(ip string) error {
	if net.ParseIP(ip) == nil {
		return fmt.Errorf("%q is not a valid IP address", ip)
	}
	return nil
}

func validateCIDR(cidr string) error {
	_, _, err := net.ParseCIDR(cidr)
	if err != nil {
		return fmt.Errorf("%q is not a valid CIDR: %w", cidr, err)
	}
	return nil
}
