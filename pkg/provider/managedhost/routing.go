package managedhost

import (
	"context"
	"fmt"

	"github.com/cruxdigital-llc/conga-line/pkg/common"
	"github.com/cruxdigital-llc/conga-line/pkg/provider"
)

// WriteRoutingJSON generates routing.json in Go (the canonical
// common.GenerateRoutingJSON) and writes it to the host through the transport.
// This is the shared replacement for per-provider bash routing mutation. The
// resolver selects the webhook target form — pass common.LoopbackWebhookResolver
// for the host-networking router topology (loopback 127.0.0.1:<hostPort>).
//
// path is provider-specific (AWS/remote: /opt/conga/config/routing.json; local:
// ~/.conga/config/routing.json), so the caller supplies it.
func WriteRoutingJSON(ctx context.Context, t Transport, path string, agents []provider.AgentConfig, resolver common.WebhookTargetResolver) error {
	data, err := common.GenerateRoutingJSON(agents, resolver)
	if err != nil {
		return fmt.Errorf("generate routing.json: %w", err)
	}
	if err := t.PutFile(ctx, path, data, 0o644); err != nil {
		return fmt.Errorf("write routing.json to %s: %w", path, err)
	}
	return nil
}
