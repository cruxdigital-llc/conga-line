package scripts

import (
	"strings"
	"testing"
	"text/template"
)

func TestDeployEgressScriptTemplateRender(t *testing.T) {
	tmpl, err := template.New("deploy-egress").Parse(DeployEgressScript)
	if err != nil {
		t.Fatalf("failed to parse deploy-egress template: %v", err)
	}

	data := struct {
		AgentName        string
		Mode             string
		PolicyContent    string
		EnvoyConfig      string
		ProxyBootstrapJS string
	}{
		AgentName: "testagent",
		Mode:      "enforce",
		PolicyContent: `apiVersion: conga.dev/v1alpha1
egress:
  allowed_domains:
    - api.anthropic.com
    - "*.slack.com"
  mode: enforce`,
		EnvoyConfig:      "static_resources:\n  listeners:\n    - name: main\n",
		ProxyBootstrapJS: "const http = require('http');\n",
	}

	var buf strings.Builder
	if err := tmpl.Execute(&buf, data); err != nil {
		t.Fatalf("failed to execute deploy-egress template: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "testagent") {
		t.Error("expected agent name in rendered output")
	}
	if !strings.Contains(output, "enforce") {
		t.Error("expected mode in rendered output")
	}
	if !strings.Contains(output, "api.anthropic.com") {
		t.Error("expected policy content in rendered output")
	}
	if !strings.Contains(output, "static_resources") {
		t.Error("expected envoy config in rendered output")
	}
	if !strings.Contains(output, "set -euo pipefail") {
		t.Error("expected bash strict mode in rendered output")
	}
}

func TestDeployEgressScriptValidateModeOmitsIptables(t *testing.T) {
	tmpl, err := template.New("deploy-egress").Parse(DeployEgressScript)
	if err != nil {
		t.Fatalf("failed to parse deploy-egress template: %v", err)
	}

	data := struct {
		AgentName        string
		Mode             string
		PolicyContent    string
		EnvoyConfig      string
		ProxyBootstrapJS string
	}{
		AgentName: "testagent",
		Mode:      "validate",
		PolicyContent: `apiVersion: conga.dev/v1alpha1
egress:
  allowed_domains:
    - api.anthropic.com
  mode: validate`,
		EnvoyConfig:      "static_resources:\n  listeners:\n    - name: main\n",
		ProxyBootstrapJS: "const http = require('http');\n",
	}

	var buf strings.Builder
	if err := tmpl.Execute(&buf, data); err != nil {
		t.Fatalf("failed to execute deploy-egress template: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, `EGRESS_MODE="validate"`) {
		t.Error("expected EGRESS_MODE=validate in rendered output")
	}
	// The iptables enforce block is guarded by: if [ "$EGRESS_MODE" = "enforce" ]; then
	// Verify the guard is present so iptables rules won't execute in validate mode.
	if !strings.Contains(output, `if [ "$EGRESS_MODE" = "enforce" ]`) {
		t.Error("expected enforce-mode guard for iptables rules")
	}
	// Verify cleanup section (iptables -D) is NOT guarded — it should always run
	if !strings.Contains(output, "iptables -D DOCKER-USER") {
		t.Error("expected iptables cleanup rules (iptables -D) in all modes")
	}
}

func TestRefreshUserScriptTemplateRender(t *testing.T) {
	tmpl, err := template.New("refresh-user").Parse(RefreshUserScript)
	if err != nil {
		t.Fatalf("failed to parse refresh-user template: %v", err)
	}

	data := struct {
		AWSRegion string
		AgentName string
	}{
		AWSRegion: "us-east-1",
		AgentName: "testagent",
	}

	var buf strings.Builder
	if err := tmpl.Execute(&buf, data); err != nil {
		t.Fatalf("failed to execute refresh-user template: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "testagent") {
		t.Error("expected agent name in rendered output")
	}
	if !strings.Contains(output, "us-east-1") {
		t.Error("expected AWS region in rendered output")
	}
}

func TestAddUserScriptTemplateRender(t *testing.T) {
	tmpl, err := template.New("add-user").Parse(AddUserScript)
	if err != nil {
		t.Fatalf("failed to parse add-user template: %v", err)
	}

	data := struct {
		AgentName, SlackMemberID, SlackChannel, AWSRegion, StateBucket string
		GatewayPort                                                    int
		EnvoyConfig, EgressMode, ProxyBootstrapJS                      string
	}{
		AgentName:        "testuser",
		SlackMemberID:    "U1234",
		AWSRegion:        "us-east-1",
		StateBucket:      "my-bucket",
		GatewayPort:      18789,
		EnvoyConfig:      "static_resources:\n  listeners:\n    - port: 3128\n",
		EgressMode:       "enforce",
		ProxyBootstrapJS: "const http = require('http');\n",
	}

	var buf strings.Builder
	if err := tmpl.Execute(&buf, data); err != nil {
		t.Fatalf("failed to execute add-user template: %v", err)
	}

	output := buf.String()
	checks := map[string]string{
		"agent name":            "testuser",
		"egress mode":           `EGRESS_MODE="enforce"`,
		"envoy config":          "static_resources",
		"proxy bootstrap":       "require('http')",
		"HTTPS_PROXY":           "HTTPS_PROXY=http://",
		"proxy bootstrap mount": "$BOOTSTRAP_PATH:/opt/proxy-bootstrap.js",
		"iptables guard":        `if [ "$EGRESS_MODE" = "enforce" ]`,
		"egress proxy run":      "conga-egress-proxy",
	}
	for desc, want := range checks {
		if !strings.Contains(output, want) {
			t.Errorf("expected %s (%q) in rendered output", desc, want)
		}
	}
}

func TestAddTeamScriptTemplateRender(t *testing.T) {
	tmpl, err := template.New("add-team").Parse(AddTeamScript)
	if err != nil {
		t.Fatalf("failed to parse add-team template: %v", err)
	}

	data := struct {
		AgentName, SlackMemberID, SlackChannel, AWSRegion, StateBucket string
		GatewayPort                                                    int
		EnvoyConfig, EgressMode, ProxyBootstrapJS                      string
	}{
		AgentName:        "testteam",
		SlackChannel:     "C5678",
		AWSRegion:        "us-west-2",
		StateBucket:      "team-bucket",
		GatewayPort:      18790,
		EnvoyConfig:      "static_resources:\n  listeners:\n    - port: 3128\n",
		EgressMode:       "enforce",
		ProxyBootstrapJS: "const http = require('http');\n",
	}

	var buf strings.Builder
	if err := tmpl.Execute(&buf, data); err != nil {
		t.Fatalf("failed to execute add-team template: %v", err)
	}

	output := buf.String()
	checks := map[string]string{
		"agent name":       "testteam",
		"egress mode":      `EGRESS_MODE="enforce"`,
		"envoy config":     "static_resources",
		"HTTPS_PROXY":      "HTTPS_PROXY=http://",
		"iptables guard":   `if [ "$EGRESS_MODE" = "enforce" ]`,
		"egress proxy run": "conga-egress-proxy",
		"channel routing":  "channels",
	}
	for desc, want := range checks {
		if !strings.Contains(output, want) {
			t.Errorf("expected %s (%q) in rendered output", desc, want)
		}
	}
}
