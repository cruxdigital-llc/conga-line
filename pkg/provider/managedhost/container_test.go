package managedhost

import (
	"strings"
	"testing"
)

func baseContainer() AgentContainer {
	return AgentContainer{
		Name:          "conga-demo",
		Image:         "ghcr.io/openclaw/openclaw:2026.6.5",
		Network:       "conga-demo",
		IP:            "10.99.2.2",
		HostPort:      18791,
		ContainerPort: 18789,
		DataDir:       "/opt/conga/data/demo",
		EnvFile:       "/opt/conga/config/demo.env",
		MemoryLimit:   "2g",
		CPULimit:      "0.75",
		PidsLimit:     256,
		User:          "1000:1000",
	}
}

func TestAgentContainer_CoreArgs(t *testing.T) {
	got := strings.Join(baseContainer().Args(), " ")
	for _, want := range []string{
		"/usr/bin/docker run",
		"--name conga-demo",
		"--network conga-demo",
		"--ip 10.99.2.2",
		"-p 127.0.0.1:18791:18789",
		"--cap-drop ALL",
		"--security-opt no-new-privileges",
		"--memory 2g",
		"--cpus 0.75",
		"--pids-limit 256",
		"--user 1000:1000",
		"-v /opt/conga/data/demo:/home/node/.openclaw:rw",
		"--env-file /opt/conga/config/demo.env",
		"-e NODE_OPTIONS=" + DefaultNodeOptions,
		"ghcr.io/openclaw/openclaw:2026.6.5",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("RunCmd missing %q\n got: %s", want, got)
		}
	}
	// Secrets must never be inlined as -e KEY=VALUE — they come via --env-file.
	if strings.Contains(got, "SLACK_BOT_TOKEN") || strings.Contains(got, "ANTHROPIC") {
		t.Error("RunCmd inlines a secret env var — secrets must travel via --env-file only")
	}
	// No egress proxy wiring when EgressProxyName is empty.
	if strings.Contains(got, "HTTPS_PROXY") || strings.Contains(got, "proxy-bootstrap") {
		t.Error("RunCmd wired egress proxy without EgressProxyName set")
	}
}

func TestAgentContainer_EgressProxyWiring(t *testing.T) {
	c := baseContainer()
	c.EgressProxyName = "conga-egress-demo"
	c.ProxyBootstrapPath = "/opt/conga/config/proxy-bootstrap.js"
	got := strings.Join(c.Args(), " ")
	for _, want := range []string{
		"-e HTTPS_PROXY=http://conga-egress-demo:3128",
		"-e HTTP_PROXY=http://conga-egress-demo:3128",
		"-e NO_PROXY=localhost,127.0.0.1",
		"-v /opt/conga/config/proxy-bootstrap.js:/opt/proxy-bootstrap.js:ro",
		"--require /opt/proxy-bootstrap.js",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("egress RunCmd missing %q\n got: %s", want, got)
		}
	}
	// The image must remain the final argument (docker run [opts] IMAGE).
	args := c.Args()
	if args[len(args)-1] != c.Image {
		t.Errorf("image must be the final argv element, got %q", args[len(args)-1])
	}
}

// TestSystemdExecStart_QuotesNodeOptions guards the systemd-parsing hazard: the
// NODE_OPTIONS value contains a space (--require suffix), so it MUST be
// double-quoted or systemd would split it and docker would see --require as a
// stray flag. This is the exact bug the bash unit avoided with NODE_OPTIONS="...".
func TestSystemdExecStart_QuotesNodeOptions(t *testing.T) {
	c := baseContainer()
	c.EgressProxyName = "conga-egress-demo"
	c.ProxyBootstrapPath = "/opt/conga/config/proxy-bootstrap.js"
	exec := SystemdExecStart(c.Args())
	want := `-e "NODE_OPTIONS=--max-old-space-size=1536 --require /opt/proxy-bootstrap.js"`
	if !strings.Contains(exec, want) {
		t.Errorf("NODE_OPTIONS must be double-quoted as one systemd arg\n want substring: %s\n got: %s", want, exec)
	}
	// Args without whitespace must NOT be quoted (keeps the unit readable + matches
	// the prior bash unit's shape).
	if strings.Contains(exec, `"--name"`) || strings.Contains(exec, `"conga-demo"`) {
		t.Error("SystemdExecStart over-quoted a whitespace-free argument")
	}
}
