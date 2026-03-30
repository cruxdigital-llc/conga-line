# Tasks — Non-Root Container Enforcement

## Phase 1: Agent containers — `--user 1000:1000`
- [x] 1.1 Local provider: `localprovider/docker.go` — `runAgentContainer()`
- [x] 1.2 Remote provider: `remoteprovider/docker.go` — `runAgentContainer()`
- [x] 1.3 AWS bootstrap: `terraform/user-data.sh.tftpl` — agent systemd ExecStart
- [x] 1.4 AWS add-user: `cli/scripts/add-user.sh.tmpl` — ExecStart
- [x] 1.5 AWS add-team: `cli/scripts/add-team.sh.tmpl` — ExecStart
- [x] 1.6 AWS refresh-user: `cli/scripts/refresh-user.sh.tmpl` — sed replacement

## Phase 2: Router containers — `--user 1000:1000`
- [x] 2.1 Local provider: `localprovider/docker.go` — `runRouterContainer()`
- [x] 2.2 Remote provider: `remoteprovider/docker.go` — `runRouterContainer()`
- [x] 2.3 AWS bootstrap: `terraform/user-data.sh.tftpl` — router systemd ExecStart (+ missing `--tmpfs`)

## Phase 3: Documentation
- [x] 3.1 Security standards: `product-knowledge/standards/security.md` — update non-root row

## Phase 4: Verification
- [x] 4.1 Compile: `go build ./...` — success
- [x] 4.2 Unit tests: `go test ./...` — 17 packages pass
