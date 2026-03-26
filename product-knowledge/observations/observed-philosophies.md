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
