package awsprovider

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/cruxdigital-llc/conga-line/pkg/provider/managedhost"
)

// ssmTransport adapts the AWS provider's SSM primitives to the managedhost.Transport
// seam, bound to one instance. It wraps the existing uploadFile/runOnInstance so the
// shared engine can generate artifacts in Go and ship them over SSM — the same
// contract the remote provider satisfies over SSH.
type ssmTransport struct {
	p          *AWSProvider
	instanceID string
}

// transport returns a managedhost.Transport bound to instanceID.
func (p *AWSProvider) transport(instanceID string) managedhost.Transport {
	return &ssmTransport{p: p, instanceID: instanceID}
}

// compile-time check.
var _ managedhost.Transport = (*ssmTransport)(nil)

func (t *ssmTransport) PutFile(ctx context.Context, path string, content []byte, mode os.FileMode) error {
	// uploadFile takes an octal mode string (e.g. "0644") and writes atomically.
	return t.p.uploadFile(ctx, t.instanceID, path, content, fmt.Sprintf("%04o", mode.Perm()))
}

func (t *ssmTransport) RunCommand(ctx context.Context, cmd string) (string, error) {
	// SSM requires timeoutSeconds >= 30; the timeout is a completion ceiling, so
	// fast commands still return immediately. 120s accommodates the one
	// legitimately-slow managed-host command: `systemctl restart`, which blocks on
	// the unit's plugin-install ExecStartPre (npm install runs full on a fresh
	// agent's empty data dir, ~30-60s; a no-op once the plugin is on disk). The
	// payload stays a tiny command string — the SSM-discipline constraint is about
	// not streaming large scripts, not wall-clock. Matches the prior bash path's 120s.
	res, err := t.p.runOnInstance(ctx, t.instanceID, cmd, 120*time.Second)
	if err != nil {
		return "", err
	}
	if res.Status != "Success" {
		return res.Stdout, fmt.Errorf("command failed (status=%s): %s", res.Status, res.Stderr)
	}
	return res.Stdout, nil
}

func (t *ssmTransport) ReadFile(ctx context.Context, path string) ([]byte, error) {
	// SSM has no native file transfer; base64-encode on the host and decode here
	// (mirrors uploadFile's transmission strategy in reverse).
	res, err := t.p.runOnInstance(ctx, t.instanceID, fmt.Sprintf("base64 -w0 %q", path), 30*time.Second)
	if err != nil {
		return nil, err
	}
	if res.Status != "Success" {
		return nil, fmt.Errorf("failed to read %s (status=%s): %s", path, res.Status, res.Stderr)
	}
	return base64.StdEncoding.DecodeString(strings.TrimSpace(res.Stdout))
}
