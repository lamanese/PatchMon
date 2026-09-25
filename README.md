# amanIT Patch Management

**An independent fork of [PatchMon](https://github.com/PatchMon/PatchMon), operated and extended by [amanIT GmbH](https://amanit.swiss).**

This repository is not affiliated with, endorsed by or supported by PatchMon Ltd. "PatchMon" and the PatchMon logo are trademarks of PatchMon Ltd; the fork is distributed and operated under its own name. See [NOTICE](NOTICE) for attribution and the notice of changes required by the AGPL.

Licensed under the [GNU Affero General Public License v3](LICENSE). If you use an instance of this software over a network you are entitled to the source code of exactly the version you are using: every release is tagged (`v<upstream-base>-am.<n>`, for example `v2.0.2-am.11`), the running version is shown under *Settings → Server Version* together with a link to its tag, and the login page links to the [tag list](https://github.com/lamanese/PatchMon/tags).

## What it is

A self-hosted patch and server management platform for Linux and Windows hosts: a Go server backed by PostgreSQL and Redis, a React frontend and a small Go agent per host. The upstream project describes the base feature set in its own documentation; this README concentrates on what the fork changes.

## Changes compared to upstream PatchMon 2.0.2

The fork is a hard fork of upstream version 2.0.2. Upstream changes are not merged wholesale; individual security and reliability fixes are picked and documented in the Git history.

Operations
- **Remote reboot** with per-host allow list, server self-exclusion, RBAC permission `can_reboot_hosts` and audit trail.
- **Reboot scheduler** and **patch scheduler** (once/daily/weekly, per-schedule timezone, host groups) with dispatch guards and a reaper for orphaned runs.
- **Windows patching** via Windows Update Agent and WinGet, including agent self-update on Windows, dry runs, honest failure status and install heartbeat.
- **apt hardening**: `--with-new-pkgs` upgrades, local config always kept, copyable repair hints in the patch output. The agent never runs risky repairs on its own.
- **Host boot time** reporting, container-aware.

Reporting
- **Customer reports** per host group: strict definitions (language de/en, period 7/30/90 days), fail-closed scoping, HTML/CSV/PDF output with embedded fonts, PDF preview without sending.

Security and operation
- Client-IP resolution behind trusted proxies (`TRUSTED_PROXY_RANGES`), auth hardening, failed-login logging, host licensing (`PM_LICENSE_*`).
- **Fork mode** (`PM_HIDE_COMMUNITY_LINKS=true`): no upstream links, no version beacons, no telemetry. `PM_DISABLE_SIGNUP`, `PM_IGNORE_DEFINITION_UPDATES`.
- Own migration set (`migrations_fork/`, table `schema_migrations_fork`) kept separate from upstream migrations.

## Deployment

The fork is built by GitHub Actions into a public image:

```
ghcr.io/lamanese/patchmon-server:latest      # deploy branch feat/remote-reboot
ghcr.io/lamanese/patchmon-server:feat-<name> # every feature branch
```

`docker/docker-compose.fork.yml` is the production-style stack (PostgreSQL, Redis, guacd, server). `docker/server-fork.Dockerfile` builds the image from public base images only. Agent binaries for Linux (amd64/arm64/386/arm), FreeBSD and Windows are bundled in the server image and self-update from there.

Configuration is done through `docker/.env`; the relevant variables are documented in `docs/patchmon-admin-guide.md`.

## Repository layout

| Path | Content |
|---|---|
| `server-source-code/` | Go server (API, queue workers, migrations, reports) |
| `agent-source-code/` | Go agent |
| `frontend/` | React frontend |
| `docs/` | Administrator, operator and API/integration guides (fork edition) |
| `docker/` | Fork Dockerfile and compose files |
| `.github/workflows/build-lamanese.yml` | CI: agent cross-compile, migration tests against PostgreSQL, image build |

## Branches

- `feat/remote-reboot` is the deploy branch (GitHub default branch). Every push builds `:latest`.
- `feat/remote-reboot-base` is the branch of the upstream pull request #837 (basic remote reboot only).
- `main` is a frozen copy of upstream `main` from May 2026 and is not deployed.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md). Contributions to the upstream project belong at [PatchMon/PatchMon](https://github.com/PatchMon/PatchMon).

## License

GNU Affero General Public License v3. Copyright (c) PatchMon Ltd for the original work, Copyright (c) 2026 amanIT GmbH for the modifications. See [LICENSE](LICENSE) and [NOTICE](NOTICE).
