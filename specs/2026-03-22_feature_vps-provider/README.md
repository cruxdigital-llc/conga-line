# VPS Provider — Trace Log

**Feature**: `vps-provider`
**Created**: 2026-03-22
**Phase**: Specified — Ready for Implementation

## Session Log

### 2026-03-22 — Plan Feature

**Active Personas**: Architect, Product Manager, QA
**Active Capabilities**: Playwright (browser testing), Context7 (library docs)

#### Decisions
- SSH-only provider (no VPS API integrations) — user provisions their own VM
- Docker auto-install during setup (detect OS: apt/dnf/yum/pacman)
- File-based secrets on remote host (mode 0400), same model as local provider
- SSH tunnel for gateway access — no inbound ports beyond SSH (22)
- New dependency: `golang.org/x/crypto` for SSH client + `github.com/pkg/sftp` for file transfer
- Remote directory at `/opt/conga/` (matches AWS provider path)
- SSH key auth only (no password auth)

#### Files Created
- [requirements.md](requirements.md) — Feature requirements and user stories
- [plan.md](plan.md) — High-level implementation plan

### 2026-03-22 — Spec Feature

#### Files Created
- [spec.md](spec.md) — Detailed technical specification (12 sections)

#### Persona Review

**Architect**: APPROVE — architecture consistent with existing provider model. Two new dependencies justified. No API contract changes. `shelljoin()` is key new primitive requiring thorough testing.

**Product Manager**: APPROVE — clear user value (cloud agents at $5-10/mo without AWS), well-scoped (no VPS API integrations), 14 user stories mapped to interface methods.

**QA**: APPROVE with notes:
- Test SSH agent passphrase handling edge case
- Verify SFTP fallback on minimal VPS images
- Verify Docker install script on Ubuntu, Debian, CentOS, Fedora
- `shelljoin()` needs comprehensive unit tests (empty strings, special chars, newlines)
- Manual VPS testing acceptable for initial implementation; CI integration is follow-up

#### Standards Gate Report
| Standard | Severity | Verdict |
|---|---|---|
| Zero trust the AI agent | must | ✅ PASSES |
| Immutable configuration | must | ⚠️ WARNING — no filesystem-level immutability (same as local provider, SHA256 monitoring) |
| Least privilege everywhere | must | ✅ PASSES |
| Defense in depth | must | ✅ PASSES |
| Secrets never touch disk | must | ⚠️ WARNING — file-based secrets (same as local provider, accepted trade-off) |
| Detect what you can't prevent | must | ✅ PASSES |
| Isolated Docker networks | must | ✅ PASSES |
| Container resource limits | must | ✅ PASSES |
| Drop all capabilities | must | ✅ PASSES |

**Gate Decision**: PASS (0 violations, 2 warnings — both are existing accepted trade-offs shared with local provider)
