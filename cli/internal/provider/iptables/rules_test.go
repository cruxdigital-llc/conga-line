package iptables

import (
	"strings"
	"testing"
)

func TestAddRulesCmd(t *testing.T) {
	cmd, err := AddRulesCmd("172.18.0.2", "172.18.0.0/16")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify all three rules are present with check-before-insert pattern
	checks := []string{
		"iptables -C DOCKER-USER -s 172.18.0.2 -j DROP",
		"iptables -I DOCKER-USER -s 172.18.0.2 -j DROP",
		"iptables -C DOCKER-USER -s 172.18.0.2 -d 172.18.0.0/16 -j RETURN",
		"iptables -I DOCKER-USER -s 172.18.0.2 -d 172.18.0.0/16 -j RETURN",
		"iptables -C DOCKER-USER -s 172.18.0.2 -m conntrack --ctstate ESTABLISHED,RELATED -j RETURN",
		"iptables -I DOCKER-USER -s 172.18.0.2 -m conntrack --ctstate ESTABLISHED,RELATED -j RETURN",
	}
	for _, c := range checks {
		if !strings.Contains(cmd, c) {
			t.Errorf("AddRulesCmd missing: %s", c)
		}
	}
}

func TestAddRulesCmdInvalidIP(t *testing.T) {
	_, err := AddRulesCmd("not-an-ip", "172.18.0.0/16")
	if err == nil {
		t.Fatal("expected error for invalid IP")
	}
	if !strings.Contains(err.Error(), "invalid container IP") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestAddRulesCmdInvalidCIDR(t *testing.T) {
	_, err := AddRulesCmd("172.18.0.2", "not-a-cidr")
	if err == nil {
		t.Fatal("expected error for invalid CIDR")
	}
	if !strings.Contains(err.Error(), "invalid subnet CIDR") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestRemoveRulesCmd(t *testing.T) {
	cmd, err := RemoveRulesCmd("172.18.0.2", "172.18.0.0/16")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	checks := []string{
		"iptables -D DOCKER-USER -s 172.18.0.2 -m conntrack --ctstate ESTABLISHED,RELATED -j RETURN",
		"iptables -D DOCKER-USER -s 172.18.0.2 -d 172.18.0.0/16 -j RETURN",
		"iptables -D DOCKER-USER -s 172.18.0.2 -j DROP",
	}
	for _, c := range checks {
		if !strings.Contains(cmd, c) {
			t.Errorf("RemoveRulesCmd missing: %s", c)
		}
	}

	// All deletions should be wrapped with || true
	parts := strings.Split(cmd, ";")
	for _, p := range parts {
		if !strings.Contains(p, "|| true") {
			t.Errorf("RemoveRulesCmd part missing || true: %s", p)
		}
	}
}

func TestRemoveRulesCmdEmptyIP(t *testing.T) {
	cmd, err := RemoveRulesCmd("", "172.18.0.0/16")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cmd != "" {
		t.Errorf("expected empty string for empty IP, got: %s", cmd)
	}
}

func TestRemoveRulesCmdInvalidIP(t *testing.T) {
	_, err := RemoveRulesCmd("not-an-ip", "172.18.0.0/16")
	if err == nil {
		t.Fatal("expected error for invalid IP")
	}
	if !strings.Contains(err.Error(), "invalid container IP") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestRemoveRulesCmdInvalidCIDR(t *testing.T) {
	_, err := RemoveRulesCmd("172.18.0.2", "not-a-cidr")
	if err == nil {
		t.Fatal("expected error for invalid CIDR")
	}
	if !strings.Contains(err.Error(), "invalid subnet CIDR") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestCheckRulesCmd(t *testing.T) {
	cmd, err := CheckRulesCmd("172.18.0.2", "172.18.0.0/16")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should use -C (check) for all three rules joined with &&
	if strings.Count(cmd, "iptables -C") != 3 {
		t.Errorf("expected 3 iptables -C checks, got cmd: %s", cmd)
	}
	if strings.Count(cmd, "&&") != 2 {
		t.Errorf("expected 2 && conjunctions for all-must-pass semantics, got cmd: %s", cmd)
	}
	if strings.Contains(cmd, "|| true") {
		t.Error("CheckRulesCmd should not use || true — failures mean rules are missing")
	}
}

func TestCheckRulesCmdInvalidIP(t *testing.T) {
	_, err := CheckRulesCmd("not-an-ip", "172.18.0.0/16")
	if err == nil {
		t.Fatal("expected error for invalid IP")
	}
	if !strings.Contains(err.Error(), "invalid container IP") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestCheckRulesCmdInvalidCIDR(t *testing.T) {
	_, err := CheckRulesCmd("172.18.0.2", "not-a-cidr")
	if err == nil {
		t.Fatal("expected error for invalid CIDR")
	}
	if !strings.Contains(err.Error(), "invalid subnet CIDR") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestAddRulesCmdDeterministic(t *testing.T) {
	cmd1, _ := AddRulesCmd("10.0.0.5", "10.0.0.0/24")
	cmd2, _ := AddRulesCmd("10.0.0.5", "10.0.0.0/24")
	if cmd1 != cmd2 {
		t.Error("AddRulesCmd should be deterministic")
	}
}
