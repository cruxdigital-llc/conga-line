# Observed Philosophies

*This file is populated automatically by the `pattern-observer` module during normal workflow execution.*
*Items here are reviewed and promoted (or discarded) during `/glados/recombobulate`.*

---

<!-- Add observations below this line -->

### 2026-03-16 - Self-service over admin bottlenecks
- **Source**: User preference during onboarding design
- **Context**: Each decision point (secrets, skills, restarts) was pushed toward user self-service rather than requiring admin intervention
- **Proposed Philosophy**: "Users should be able to manage their own agent lifecycle without admin involvement wherever security allows"
- **Suggested Weight**: preferred
- **Suggested Domain**: operations, onboarding
- **Confidence**: High
- **Status**: promoted — aligns with "default to warn, offer enforce" principle in portable-policy.md

### 2026-03-25 - Default to warn, offer enforce
- **Source**: Explicit design principle in portable-policy.md Appendix A
- **Context**: Local provider defaults to validate mode (warns about unenforced policy) rather than blocking agent startup. Operator chooses enforce mode when ready.
- **Proposed Philosophy**: "In validate mode, the provider warns about unenforced policy rules without blocking agent startup. The operator decides whether that's acceptable. Conga Line respects their judgment about which mode fits their workflow."
- **Suggested Weight**: core
- **Suggested Domain**: security, ux
- **Confidence**: High
- **Status**: pending

### 2026-03-25 - The universal baseline grows slowly
- **Source**: Explicit design principle in portable-policy.md Appendix A
- **Context**: Every control added to all providers is another thing that can break and another barrier to adoption. The competitive advantage at lower tiers is simplicity.
- **Proposed Philosophy**: "The baseline should only grow with controls that are invisible to the user. The floor must never become a wall."
- **Suggested Weight**: core
- **Suggested Domain**: architecture, security
- **Confidence**: High
- **Status**: promoted — reflected in security.md Universal Baseline section and architecture.md principle #4 (no enforcement without policy)

### 2026-06-10 - Show source-of-truth, don't re-implement upstream semantics
- **Source**: Operator decision during #31 P7 (`conga agent show-config`) — chose a "layered view" (the 4 deployed `$include` files, labeled by precedence) over synthesizing the merged effective config in Go.
- **Context**: Rendering the "effective merged config" would require replicating OpenClaw's deep-merge (root-wins, array later-wins, object union, array handling). A subtly-wrong synthesized merge would mislead operators — worse than not having it. The authoritative merge lives in OpenClaw; Conga shows the inputs + precedence rules instead.
- **Proposed Philosophy**: "When a downstream tool (OpenClaw, Envoy, Docker) owns a computation, surface its inputs and our precedence contract rather than re-deriving its output operator-side. A faithful view of the source-of-truth beats a synthesized view that can drift from reality."
- **Suggested Weight**: preferred
- **Suggested Domain**: architecture, ux
- **Confidence**: Medium
- **Status**: pending

### 2026-06-13 - Logic in tested Go behind thin seams, not templated bash
- **Source**: Explicit, repeated operator statements across the managed-host-provisioning-engine session ("be opinionated about where we contain logic, how we test it, and how consistently it is reused"; "remove as much bash as possible because it is hard to test and error prone").
- **Context**: Choosing the convergence strategy for AWS provisioning (Theme 3 of the codebase audit). The operator consistently favored moving host-provisioning logic out of hand-maintained, untestable templated bash into shared Go exercised by unit tests, with provider-specific concerns isolated behind minimal seams (transport, host-supervisor).
- **Proposed Philosophy**: "Host/provisioning logic belongs in shared, unit-tested Go behind thin provider seams — not in templated bash. Bash on a host should place files and run short commands, never carry logic that can drift or fail untested. Generating an artifact (emit a struct/typed value, assert on it) is preferred over emitting bash strings, because it is testable without a host."
- **Suggested Weight**: core
- **Suggested Domain**: architecture, testing
- **Confidence**: High
- **Status**: adopted — realized by the managed-host engine (`pkg/provider/managedhost/`, PR #67) and its hardening (PR #68). AWS + remote now provision through the same tested Go engine over a `Transport` seam; `refresh-user.sh.tmpl` was deleted; the network migration moved from a shell string to `ReconcileAgentNetwork` (unit-tested). The C5b live reboot acceptance validated the result end-to-end.
