# Tasks: EC2 + Docker Bootstrap

- [x] Task 1: Write `terraform/user-data.sh.tftpl`
- [x] Task 2: Write `terraform/compute.tf`
- [x] Task 3: Update `terraform/outputs.tf`
- [x] Task 4: `terraform plan` — 2 resources (launch template + instance)
- [x] Task 5: `terraform apply` — instance deployed
- [x] Task 6: Bootstrap completed, SSM accessible
- [x] Task 7: Container running, healthy, Slack connected

## Issues Resolved During Implementation
1. **AL2023 missing `uidmap`/`fuse-overlayfs`**: Packages named differently (`shadow-utils-subid`), but full rootless Docker deps not available
2. **Docker CE rootless on AL2023**: Docker CE Fedora repo doesn't map cleanly to AL2023; `fuse-overlayfs` and `slirp4netns` missing. **Decision: use standard Docker with all other hardening controls. Defer rootless to Horizon 3.**
3. **uid 1000 collision**: AL2023's `ec2-user` is uid 1000. Created `conga` user without fixed uid.
4. **`loginctl enable-linger` failed**: `systemd-logind` not started by default. Fixed by starting it first.
5. **EROFS / read-only filesystem**: Conga Line hot-reload writes `.tmp` files next to `openclaw.json`. Cannot mount config as `:ro`. Fixed by mounting entire `.conga` dir writable with config owned by container user.
6. **OOM crash**: Node.js heap limit too low for 1536MB container. Fixed by adding `NODE_OPTIONS="--max-old-space-size=1536"` and bumping container to 2GB.
7. **Permission denied on volume**: Standard Docker maps container uid 1000 directly to host uid 1000. Must `chown 1000:1000` the data directory.

## Final Instance
- ID: i-04cc46d9ee64897b9
- Slack: socket mode connected, channel CEXAMPLE01 resolved
- Container: healthy, 3+ minutes uptime
