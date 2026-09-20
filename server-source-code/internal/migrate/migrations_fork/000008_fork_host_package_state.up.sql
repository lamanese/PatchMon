-- Adds the "package manager is in a broken state" indicator to hosts. The agent
-- (2.0.15+) reports it when `dpkg --audit` finds half-installed or unconfigured
-- packages. It is a hint only: PatchMon never repairs this itself, an
-- administrator has to finish the installation in a terminal on the host.
ALTER TABLE hosts
    ADD COLUMN IF NOT EXISTS fork_pkg_broken BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN IF NOT EXISTS fork_pkg_broken_detail TEXT;
