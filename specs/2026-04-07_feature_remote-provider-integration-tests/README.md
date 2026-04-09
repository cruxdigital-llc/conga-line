# Remote Provider Integration Tests

Extend the CLI integration test suite to exercise all primary use cases
through the remote provider's SSH+SFTP code paths. Uses a local SSH
container with the Docker socket mounted — no external infrastructure.

## Session log
- 2026-04-07: Feature planning started via `/glados/plan-feature`.
  Prior art: local provider integration tests (specs/2026-04-07_feature_cli-integration-tests/).
- 2026-04-07: Spec session started via `/glados/spec-feature`.
- 2026-04-07: Spec written. Persona review: QA + Architect approve.
  Standards gate: 0 violations.

## Active Capabilities
- Bash (go build, go test, docker)

## Active Personas
- QA (test coverage, edge cases)
- Architect (isolation, Docker-in-Docker, SSH container design)
