# Implementation Tasks — Portable Egress Policy Compliance (Pass 2)

## Phase 2: All Providers — Split proxy deployment from iptables enforcement
- [x] 2.1 Local provider: split `egressEnforce` into `egressProxy` + `egressEnforce` in ProvisionAgent
- [x] 2.2 Local provider: split in RefreshAgent
- [x] 2.3 Remote provider: split in ProvisionAgent
- [x] 2.4 Remote provider: split in RefreshAgent
- [x] 2.5 Run `go build` to verify compilation

## Phase 3: AWS Bootstrap — Always deploy proxy, iptables in enforce only
- [x] 3.1 Update `generate_egress_conf()` to always generate config (validate = keep domains for logging)
- [x] 3.2 Add validate/enforce mode to Lua deny action in bootstrap
- [x] 3.3 Gate iptables on enforce mode in bootstrap

## Phase 3b: Validate-Mode Lua Filter (Log-but-Allow)
- [x] 3b.1 Add `mode` parameter to `GenerateProxyConf()` in egress.go
- [x] 3b.2 Add `ValidateMode` to envoy config template — logWarn instead of 403
- [x] 3b.3 Update all callers of `GenerateProxyConf()`

## Phase 4: Enforcement Report — Update detail strings
- [x] 4.1 Update validate-mode detail string to reflect passthrough proxy with logging

## Phase 5: Tests & Documentation
- [x] 5.1 Update proxy config tests for validate mode output
- [x] 5.2 Update `conga-policy.yaml.example` comments
- [x] 5.3 Update `security.md` to reflect validate = proxy with logging
- [x] 5.4 Run full test suite
