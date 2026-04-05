// Package openclaw implements the Runtime interface for the OpenClaw agent.
package openclaw

import (
	"github.com/cruxdigital-llc/conga-line/pkg/runtime"
)

func init() {
	runtime.Register(runtime.RuntimeOpenClaw, func() runtime.Runtime { return &Runtime{} })
}

// Runtime is the OpenClaw implementation of the runtime.Runtime interface.
type Runtime struct{}

func (r *Runtime) Name() runtime.RuntimeName { return runtime.RuntimeOpenClaw }
