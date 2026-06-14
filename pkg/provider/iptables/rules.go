// Package iptables provides shared iptables command generation and orchestration
// for egress enforcement. Providers call the Cmd functions to get shell command
// strings and the orchestration functions (AddRules, RemoveRules, CheckRules) to
// execute them via a provider-supplied callback.
package iptables

import (
	"fmt"
	"net"
	"strings"
)

// egressRuleSpecs returns the DOCKER-USER rule specifications (everything after
// `-A DOCKER-USER`) for the given container IP + subnet, in priority order: every
// RETURN (allow) first, the terminal DROP last. All RETURNs MUST precede the DROP.
//
//  1. dst=subnet → RETURN (the per-agent Envoy proxy + Docker's in-subnet bits)
//  2. ESTABLISHED,RELATED → RETURN (response traffic)
//  3. udp/tcp dport 53 → RETURN (DNS). REQUIRED on AWS: Docker's embedded resolver
//     forwards to the VPC resolver, which lives OUTSIDE the per-agent subnet, so the
//     forwarded query is sourced from the container IP to a non-subnet address and
//     would otherwise hit the DROP. Without these the agent cannot resolve names.
//  4. DROP (block all other egress from this source — fail-closed)
func egressRuleSpecs(containerIP, subnetCIDR string) []string {
	s := "-s " + containerIP
	return []string{
		s + " -d " + subnetCIDR + " -j RETURN",
		s + " -m conntrack --ctstate ESTABLISHED,RELATED -j RETURN",
		s + " -p udp --dport 53 -j RETURN",
		s + " -p tcp --dport 53 -j RETURN",
		s + " -j DROP",
	}
}

// AddRulesCmd returns a shell command that idempotently inserts the egress rules
// into DOCKER-USER for the given container IP (check-before-insert per rule).
// Rules are inserted in reverse spec order — iptables -I pushes to the top, so
// inserting the terminal DROP first leaves it at the bottom with every RETURN above
// it. Returns an error if containerIP or subnetCIDR are not well-formed.
func AddRulesCmd(containerIP, subnetCIDR string) (string, error) {
	if err := validateIP(containerIP); err != nil {
		return "", fmt.Errorf("invalid container IP: %w", err)
	}
	if err := validateCIDR(subnetCIDR); err != nil {
		return "", fmt.Errorf("invalid subnet CIDR: %w", err)
	}
	specs := egressRuleSpecs(containerIP, subnetCIDR)
	cmds := make([]string, 0, len(specs))
	for i := len(specs) - 1; i >= 0; i-- {
		r := specs[i]
		cmds = append(cmds, fmt.Sprintf("iptables -C DOCKER-USER %s 2>/dev/null || iptables -I DOCKER-USER %s", r, r))
	}
	return strings.Join(cmds, "; "), nil
}

// RemoveRulesCmd returns a shell command that removes the egress iptables rules
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
	specs := egressRuleSpecs(containerIP, subnetCIDR)
	cmds := make([]string, 0, len(specs))
	for _, r := range specs {
		cmds = append(cmds, fmt.Sprintf("iptables -D DOCKER-USER %s 2>/dev/null || true", r))
	}
	return strings.Join(cmds, "; "), nil
}

// CheckRulesCmd returns a shell command that checks whether all egress rules exist
// for the given container IP. Exits 0 only if every rule is present.
// Returns an error if containerIP or subnetCIDR are not well-formed.
func CheckRulesCmd(containerIP, subnetCIDR string) (string, error) {
	if err := validateIP(containerIP); err != nil {
		return "", fmt.Errorf("invalid container IP: %w", err)
	}
	if err := validateCIDR(subnetCIDR); err != nil {
		return "", fmt.Errorf("invalid subnet CIDR: %w", err)
	}
	specs := egressRuleSpecs(containerIP, subnetCIDR)
	checks := make([]string, 0, len(specs))
	for _, r := range specs {
		checks = append(checks, fmt.Sprintf("iptables -C DOCKER-USER %s 2>/dev/null", r))
	}
	return strings.Join(checks, " && "), nil
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
