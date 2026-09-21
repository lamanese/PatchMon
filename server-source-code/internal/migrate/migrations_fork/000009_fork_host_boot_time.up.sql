-- Adds the host's last boot instant. Agents 2.0.20+ report it (gopsutil
-- BootTime; inside LXC derived from the container's own uptime). Older agents
-- leave it NULL and the UI falls back to the system_uptime text. It is the
-- first authoritative source for "when did this host last come up"; the
-- uptime text has no measurement timestamp and cannot be turned into a date.
ALTER TABLE hosts
    ADD COLUMN IF NOT EXISTS fork_boot_time TIMESTAMPTZ;
