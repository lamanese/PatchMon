-- name: ListRebootSchedules :many
SELECT rs.*, hg.name AS host_group_name
FROM reboot_schedules rs
JOIN host_groups hg ON hg.id = rs.host_group_id
ORDER BY rs.name;

-- name: GetRebootScheduleByID :one
SELECT * FROM reboot_schedules WHERE id = $1;

-- name: ListEnabledRebootSchedules :many
SELECT * FROM reboot_schedules WHERE enabled = true ORDER BY id;

-- name: CreateRebootSchedule :one
INSERT INTO reboot_schedules (
    id, name, host_group_id, schedule_type, run_at, weekday, time_of_day,
    timezone, only_if_required, enabled, created_by, created_at, updated_at
) VALUES (
    sqlc.arg('id'), sqlc.arg('name'), sqlc.arg('host_group_id'), sqlc.arg('schedule_type'),
    sqlc.arg('run_at'), sqlc.arg('weekday'), sqlc.arg('time_of_day'), sqlc.arg('timezone'),
    sqlc.arg('only_if_required'), sqlc.arg('enabled'), sqlc.arg('created_by'), NOW(), NOW()
) RETURNING *;

-- name: UpdateRebootSchedule :one
UPDATE reboot_schedules SET
    name = sqlc.arg('name'),
    host_group_id = sqlc.arg('host_group_id'),
    schedule_type = sqlc.arg('schedule_type'),
    run_at = sqlc.arg('run_at'),
    weekday = sqlc.arg('weekday'),
    time_of_day = sqlc.arg('time_of_day'),
    timezone = sqlc.arg('timezone'),
    only_if_required = sqlc.arg('only_if_required'),
    enabled = sqlc.arg('enabled'),
    missed_at = sqlc.arg('missed_at'),
    updated_at = NOW()
WHERE id = sqlc.arg('id')
RETURNING *;

-- name: SetRebootScheduleEnabled :one
UPDATE reboot_schedules SET enabled = sqlc.arg('enabled'), missed_at = NULL, updated_at = NOW()
WHERE id = sqlc.arg('id')
RETURNING *;

-- name: DeleteRebootSchedule :exec
DELETE FROM reboot_schedules WHERE id = $1;

-- name: ConsumeRebootScheduleSlot :one
-- Atomically claims a schedule slot (compare-and-set on last_run_at) so that
-- concurrent dispatchers cannot run the same slot twice. Returns no row when
-- the slot was already consumed. One-shot schedules are disabled in the same
-- statement so a claim can never leave them armed.
-- updated_at is intentionally left alone: it tracks config changes and the
-- dispatcher skips slots older than it (no instant fire on create/enable).
UPDATE reboot_schedules
SET last_run_at = sqlc.arg('last_run_at'),
    enabled = CASE WHEN schedule_type = 'once' THEN false ELSE enabled END
WHERE id = sqlc.arg('id')
  AND enabled = true
  AND (last_run_at IS NULL OR last_run_at < sqlc.arg('slot'))
RETURNING id;

-- name: MarkRebootScheduleMissed :exec
UPDATE reboot_schedules SET enabled = false, missed_at = sqlc.arg('missed_at'), updated_at = NOW()
WHERE id = sqlc.arg('id');

-- name: UserCanRebootHosts :one
-- Execution-time revalidation of the schedule creator: the schedule must stop
-- firing once its creator is deleted, deactivated or loses can_reboot_hosts.
SELECT u.is_active AND rp.can_reboot_hosts AS allowed
FROM users u
JOIN role_permissions rp ON rp.role = u.role
WHERE u.id = $1;

-- name: ListRebootScheduleGroupHosts :many
SELECT h.id, h.api_id, h.friendly_name, h.hostname, h.machine_id, h.allow_reboot, h.needs_reboot
FROM host_group_memberships hgm
JOIN hosts h ON h.id = hgm.host_id
WHERE hgm.host_group_id = $1
ORDER BY h.friendly_name;
