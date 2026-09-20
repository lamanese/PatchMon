-- Adds patch_schedules: recurring/one-shot scheduled patch runs scoped to one
-- host group. A schedule is either one-shot ('once', run_at) or recurring
-- ('weekly', weekday + time_of_day evaluated in the schedule's timezone).
-- At each due slot the dispatcher creates a patch_run per host in the group and
-- enqueues a run_patch task (patch_all, real run). Missed one-shot schedules
-- (server down at run_at) expire instead of running late: enabled is set to
-- false and missed_at records the slot.
--
-- This mirrors reboot_schedules (migration 000043). Unlike reboot schedules
-- there is no allowlist or server self-exclusion: patching the host that runs
-- PatchMon is a normal, safe package upgrade. The patch type is always
-- 'patch_all' (patch everything), so it is not stored as a column.

CREATE TABLE IF NOT EXISTS patch_schedules (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    host_group_id TEXT NOT NULL REFERENCES host_groups(id) ON DELETE CASCADE,
    schedule_type TEXT NOT NULL,          -- 'once' | 'weekly'
    run_at TIMESTAMP(3),                  -- for 'once'
    weekday INTEGER,                      -- 0 (Sunday) - 6 for 'weekly'
    time_of_day TEXT,                     -- 'HH:MM' for 'weekly'
    timezone TEXT NOT NULL DEFAULT 'UTC',
    enabled BOOLEAN NOT NULL DEFAULT true,
    last_run_at TIMESTAMP(3),
    missed_at TIMESTAMP(3),
    created_by TEXT REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_patch_schedules_enabled ON patch_schedules(enabled);
