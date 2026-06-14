// Package managedhost is the shared provisioning engine for managed-host
// providers (remote over SSH, AWS over SSM). It generates agent artifacts in Go
// — the same canonical generators the remote provider already uses — and ships
// them to the host through a minimal Transport seam, so the host only places
// files and runs short commands rather than executing hand-maintained bash.
//
// The engine talks to three orthogonal seams (transport, host supervisor,
// secrets/discovery); this file defines the transport. See
// specs/2026-06-13_feature_managed-host-provisioning-engine/ for the design.
package managedhost

import (
	"context"
	"os"

	"github.com/cruxdigital-llc/conga-line/pkg/provider/iptables"
)

// Transport is the seam between the engine and a specific host transport. It is
// kept deliberately small — file put, command run, file read — so a constrained
// transport (SSM: async, ≥30s minimum, no native file transfer, output
// truncation) cannot leak its constraints upward into the engine. The engine
// issues a few small PutFiles plus short RunCommands per agent; it never streams
// a large script.
//
// Implementations: remote uses SFTP + SSH; AWS uses SSM SendCommand. Both already
// have the underlying primitives.
type Transport interface {
	// PutFile writes content to path on the host with the given mode. Implementations
	// must write atomically (stage + rename) so a failure never leaves a half-written file.
	PutFile(ctx context.Context, path string, content []byte, mode os.FileMode) error

	// RunCommand runs a shell command on the host and returns its stdout.
	RunCommand(ctx context.Context, cmd string) (stdout string, err error)

	// ReadFile reads the file at path from the host. Core (not optional): used by
	// integrity/verify and the provenance view, and it keeps the door open for a
	// future `conga agent pull`.
	ReadFile(ctx context.Context, path string) ([]byte, error)
}

// ExecFuncFor adapts a Transport to the pkg/provider/iptables.ExecFunc seam, so
// the shipped iptables command-builders run unchanged over any transport. (Used
// by the egress slice; provided here as part of the seam.)
func ExecFuncFor(ctx context.Context, t Transport) iptables.ExecFunc {
	return func(cmd string) error {
		_, err := t.RunCommand(ctx, cmd)
		return err
	}
}
