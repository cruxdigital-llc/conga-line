package managedhost

import (
	"fmt"
	"strings"
)

// DefaultNodeOptions is the base NODE_OPTIONS value (V8 heap cap) every agent
// container runs with. When an egress proxy is wired in, the proxy-bootstrap
// shim is appended via --require so Node's fetch honors the CONNECT proxy.
const DefaultNodeOptions = "--max-old-space-size=1536"

// AgentContainer describes the `docker run` invocation for one agent container.
// It is the single source of the run argv shared by managed-host providers, so
// the command can't drift between provision, refresh, and the systemd unit.
//
// Secrets are passed via EnvFile (docker --env-file), never as literal
// -e KEY=VALUE on the command line: that keeps secret values out of the unit
// text and the host process table (consistent with the secrets-as-env, not
// secrets-in-config posture — #9627).
type AgentContainer struct {
	Name          string // container name, e.g. "conga-aaron"
	Image         string
	Network       string // per-agent bridge network name
	IP            string // static IP on Network — the deterministic egress source
	HostPort      int    // published loopback host port (the agent's GatewayPort)
	ContainerPort int    // in-container gateway port (openclaw ContainerPort, 18789)
	DataDir       string // host data dir bind-mounted to /home/node/.openclaw
	EnvFile       string // host env file passed via --env-file
	MemoryLimit   string // e.g. "2g"
	CPULimit      string // e.g. "0.75"
	PidsLimit     int    // e.g. 256
	User          string // e.g. "1000:1000"

	// Egress proxy wiring (optional). When EgressProxyName is set the container
	// routes HTTP(S) through the per-agent Envoy proxy; when ProxyBootstrapPath
	// is also set, the bootstrap shim is mounted and --require'd so Node's fetch
	// honors the proxy.
	EgressProxyName    string
	ProxyBootstrapPath string
}

// Args returns the full `docker run …` argv (including the leading
// "/usr/bin/docker"). The NODE_OPTIONS value is a single argv element even
// though it contains spaces — callers that render to a shell/systemd context
// must quote elements with whitespace (see SystemdExecStart).
func (c AgentContainer) Args() []string {
	args := []string{
		"/usr/bin/docker", "run",
		"--name", c.Name,
		"--network", c.Network,
		"--ip", c.IP,
		"-p", fmt.Sprintf("127.0.0.1:%d:%d", c.HostPort, c.ContainerPort),
		"--cap-drop", "ALL",
		"--security-opt", "no-new-privileges",
		"--memory", c.MemoryLimit,
		"--cpus", c.CPULimit,
		"--pids-limit", fmt.Sprintf("%d", c.PidsLimit),
		"--user", c.User,
		"-v", fmt.Sprintf("%s:/home/node/.openclaw:rw", c.DataDir),
		"--env-file", c.EnvFile,
	}

	nodeOpts := DefaultNodeOptions
	if c.EgressProxyName != "" {
		args = append(args,
			"-e", fmt.Sprintf("HTTPS_PROXY=http://%s:3128", c.EgressProxyName),
			"-e", fmt.Sprintf("HTTP_PROXY=http://%s:3128", c.EgressProxyName),
			"-e", "NO_PROXY=localhost,127.0.0.1",
		)
		if c.ProxyBootstrapPath != "" {
			args = append(args, "-v", fmt.Sprintf("%s:/opt/proxy-bootstrap.js:ro", c.ProxyBootstrapPath))
			nodeOpts += " --require /opt/proxy-bootstrap.js"
		}
	}
	// NODE_OPTIONS is passed as a -e override (highest precedence over the
	// env-file's base value) because the --require shim is only known here.
	args = append(args, "-e", "NODE_OPTIONS="+nodeOpts)
	args = append(args, c.Image)
	return args
}

// SystemdExecStart renders an argv as a systemd ExecStart value (no leading
// "ExecStart="). systemd splits an unquoted value on whitespace, so any element
// containing whitespace (e.g. the NODE_OPTIONS value with its --require suffix)
// is wrapped in double quotes — systemd treats a double-quoted run as one arg.
func SystemdExecStart(argv []string) string {
	parts := make([]string, len(argv))
	for i, a := range argv {
		if strings.ContainsAny(a, " \t") {
			parts[i] = `"` + a + `"`
		} else {
			parts[i] = a
		}
	}
	return strings.Join(parts, " ")
}
