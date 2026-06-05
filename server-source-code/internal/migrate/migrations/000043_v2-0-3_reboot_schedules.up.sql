-- Adds reboot_schedules: scheduled remote reboots scoped to one host group.
-- A schedule is either one-shot ('once', run_at) or recurring ('weekly',
-- weekday + time_of_day evaluated in the schedule's timezone). Execution
-- reuses the bulk-reboot safety layers (allow_reboot allowlist, server
-- self-exclusion, audit log); hosts that are not allowlisted are skipped.
-- Missed one-shot schedules (server down at run_at) expire instead of
-- running late: enabled is set to false and missed_at records the slot.

CREATE TABLE IF NOT EXISTS reboot_schedules (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    host_group_id TEXT NOT NULL REFERENCES host_groups(id) ON DELETE CASCADE,
    schedule_type TEXT NOT NULL,          -- 'once' | 'weekly'
    run_at TIMESTAMP(3),                  -- for 'once'
    weekday INTEGER,                      -- 0 (Sunday) - 6 for 'weekly'
    time_of_day TEXT,                     -- 'HH:MM' for 'weekly'
    timezone TEXT NOT NULL DEFAULT 'UTC',
    only_if_required BOOLEAN NOT NULL DEFAULT true,
    enabled BOOLEAN NOT NULL DEFAULT true,
    last_run_at TIMESTAMP(3),
    missed_at TIMESTAMP(3),
    created_by TEXT REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_reboot_schedules_enabled ON reboot_schedules(enabled);
