# Contributing

This repository is the independent fork of PatchMon maintained by amanIT GmbH. It is developed primarily for our own deployments; contributions are welcome but reviewed against that goal.

If your change is useful for PatchMon in general, please contribute it to the upstream project at [github.com/PatchMon/PatchMon](https://github.com/PatchMon/PatchMon) instead.

## Workflow

1. Branch from `feat/remote-reboot` (the deploy branch): `git checkout -b feat/short-description feat/remote-reboot`.
2. Make the change with tests. Go code must pass `go build`, `gofmt`, `go vet`, `go test` and `golangci-lint`; the frontend must pass Biome and `npm run build`.
3. Pushing a `feat/*` branch builds a test image `ghcr.io/lamanese/patchmon-server:feat-<name>` through GitHub Actions.
4. Open a pull request against `feat/remote-reboot`.

## Rules of the fork

- Upstream migrations (`server-source-code/internal/migrate/migrations/`) are never edited. Schema changes go into `migrations_fork/` with the `fork_` prefix for new objects.
- Every change to the agent bumps `agent-source-code/internal/pkgversion/version.go`; otherwise hosts never receive it.
- Riskier host repairs (for example `dpkg --configure -a`) are never executed by the agent; they are shown as copyable hints.
- The product name shown to users lives in `frontend/src/constants/branding.js` and the corresponding Go constants. Technical identifiers (agent binary, service names, config paths, `PM_` environment prefix, webhook header) stay as they are.
- Conventional Commits (`feat:`, `fix:`, `docs:`, `chore:`).

## License

By contributing you agree that your contribution is licensed under the GNU Affero General Public License v3, like the rest of the project.
