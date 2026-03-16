# Observed Standards

*This file is populated automatically by the `pattern-observer` module during normal workflow execution.*
*Items here are reviewed and promoted (or discarded) during `/glados/recombobulate`.*

---

<!-- Add observations below this line -->

### 2026-03-15 - S3 bucket names must include account ID
- **Source**: Implementation discovery — global namespace collision
- **Context**: `openclaw-terraform-state` was already taken; had to suffix with account ID
- **Proposed Standard**: "All S3 bucket names must be suffixed with the AWS account ID to avoid global namespace collisions"
- **Suggested Severity**: must
- **Confidence**: High
- **Status**: pending

### 2026-03-15 - Pin third-party module versions and verify provider compatibility
- **Source**: Implementation discovery — fck-nat v1.4.0 required AWS provider >= 6.0, breaking our ~> 5.0 constraint
- **Context**: `terraform init` failed until we upgraded the provider constraint
- **Proposed Standard**: "Always check third-party module provider requirements before pinning versions. Run `terraform init` early to catch version conflicts."
- **Suggested Severity**: should
- **Confidence**: High
- **Status**: pending
