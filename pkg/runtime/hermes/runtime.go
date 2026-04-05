// Package hermes implements the Runtime interface for the Hermes Agent.
package hermes

import (
	"github.com/cruxdigital-llc/conga-line/pkg/runtime"
)

func init() {
	runtime.Register(runtime.RuntimeHermes, func() runtime.Runtime { return &Runtime{} })
}

// Runtime is the Hermes Agent implementation of the runtime.Runtime interface.
type Runtime struct{}

func (r *Runtime) Name() runtime.RuntimeName { return runtime.RuntimeHermes }
