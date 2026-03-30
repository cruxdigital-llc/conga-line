# Implementation Tasks: Manifest Apply

## Phase 1: Manifest Package — Parse & Validate
- [x] 1.1 Create `cli/internal/manifest/manifest.go` — structs, Load, Validate, ExpandSecrets
- [x] 1.2 Verify it compiles: `go build ./cli/internal/manifest/...`

## Phase 2: Apply Engine
- [x] 2.1 Create `cli/internal/manifest/apply.go` — Apply orchestrator + 7 step functions
- [x] 2.2 Verify it compiles: `go build ./cli/internal/manifest/...`

## Phase 3: CLI Command
- [x] 3.1 Create `cli/cmd/apply.go` — Cobra command
- [x] 3.2 Verify full build: `go build ./cli/...`

## Phase 4: Tests
- [x] 4.1 Create `cli/internal/manifest/manifest_test.go` — 19 unit tests for Load, Validate, ExpandSecrets
- [x] 4.2 Run tests: `go test ./cli/internal/manifest/...` — all 19 pass
- [x] 4.3 Run all tests: `go test ./cli/...` — all 17 packages pass, 0 failures

## Phase 5: Demo Manifest + Docs
- [x] 5.1 Create `demo.yaml.example`
- [x] 5.2 Update DEMO.md with fast-path section
