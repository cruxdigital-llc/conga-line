<!--
GLaDOS-MANAGED DOCUMENT
Last Updated: 2026-07-22
To modify: Edit directly. These standards are expected to evolve.
-->
---
scope: [all]
severity: must
keywords: [client, customer, confidential, public, open-source, agent name, redact, PII, secret, identifier]
---

# Confidentiality Standard — Public Repository

**Severity: must** (blocking)

Conga Line is an **open-source** project. Everything in this repository — source and comments,
**commit messages**, **PR titles and descriptions**, `specs/**`, `product-knowledge/**`, test
fixtures, and any artifact a GLaDOS workflow produces — is **public-facing**. Treat it accordingly.

## The rule

**No real client, customer, deployment, agent, or person names — and no operator-/client-specific
identifiers — may appear in any committed or public-facing content.** Specifically, never include:

- **Client / customer / organization** names, or anything that identifies one — including an **agent
  named after a client** (a `foo-team` agent silently discloses that `foo` is a customer).
- Real deployed **agent names** (they reveal the operator's fleet and, by their naming, its clients).
- **People's names / handles** from a real deployment.
- Operator-/client-specific **identifiers**: AWS account IDs, EC2 instance IDs, private IPs, Slack
  channel/member IDs, hostnames, and — always — **secret values**.

Applies to: code + comments, commit messages, PR titles/bodies, `specs/**`, `product-knowledge/**`,
test fixtures, and every GLaDOS-produced artifact.

## Use placeholders instead

| Instead of | Use |
|---|---|
| a client-named / real team agent | `team-a`, `team-b`, `<team-agent>` |
| a real user agent | `<user-agent>`, `user-a` |
| a person | `<operator>` or a generic role |
| account / instance IDs, IPs, channel IDs | `<account-id>`, `i-xxxx`, `10.x.x.x`, `<channel-id>` |

When the example is a real incident, describe it **generically** ("a team agent's Linear OAuth
credential expired"). The specifics belong in operator-private stores, never the repo.

## Where real values legitimately live (NOT public)

- `terraform/**/terraform.tfvars` and `backend.tf` — **gitignored**; real agent map, secrets, bindings.
- `~/.conga/**` and the local agent-memory store — operator/agent private state.
- **Public-product references are fine**: naming a publicly-known product as a comparable/complementary
  tool (e.g. "NVIDIA OpenShell") discloses no client relationship. Naming an **agent** after that
  company **is** a disclosure and is not allowed.

## Enforcement

- **Standards gate** (`spec-feature` pre-implementation, `verify-feature` post-implementation) MUST scan
  new/changed public content for real names/identifiers and **block** on any hit (`must`).
- **Before every `git commit`, PR create/edit, `Artifact` publish, or any outward-facing send**, scrub
  for the above. This is a blocking check, not advisory.
- If you discover pre-existing violations, flag them for remediation rather than propagating them.
