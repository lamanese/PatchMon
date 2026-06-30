-- name: ListPatchSchedules :many
SELECT ps.*, hg.name AS host_group_name
FROM patch_schedules ps
JOIN host_groups hg ON hg.id = ps.host_group_id
ORDER BY ps.name;

-- name: GetPatchScheduleByID :one
SELECT * FROM patch_schedules WHERE id = $1;

-- name: ListEnabledPatchSchedules :many
SELECT * FROM patch_schedules WHERE enabled = true ORDER BY id;

-- name: CreatePatchSchedule :one
INSERT INTO patch_schedules (
    id, name, host_group_id, schedule_type, run_at, weekday, time_of_day,
    timezone, enabled, created_by, created_at, updated_at
) VALUES (
    sqlc.arg('id'), sqlc.arg('name'), sqlc.arg('host_group_id'), sqlc.arg('schedule_type'),
    sqlc.arg('run_at'), sqlc.arg('weekday'), sqlc.arg('time_of_day'), sqlc.arg('timezone'),
    sqlc.arg('enabled'), sqlc.arg('created_by'), NOW(), NOW()
) RETURNING *;

-- name: UpdatePatchSchedule :one
UPDATE patch_schedules SET
    name = sqlc.arg('name'),
    host_group_id = sqlc.arg('host_group_id'),
    schedule_type = sqlc.arg('schedule_type'),
    run_at = sqlc.arg('run_at'),
    weekday = sqlc.arg('weekday'),
    time_of_day = sqlc.arg('time_of_day'),
    timezone = sqlc.arg('timezone'),
    enabled = sqlc.arg('enabled'),
    missed_at = sqlc.arg('missed_at'),
    updated_at = NOW()
WHERE id = sqlc.arg('id')
RETURNING *;

-- name: SetPatchScheduleEnabled :one
UPDATE patch_schedules SET enabled = sqlc.arg('enabled'), missed_at = NULL, updated_at = NOW()
WHERE id = sqlc.arg('id')
RETURNING *;

-- name: DeletePatchSchedule :exec
DELETE FROM patch_schedules WHERE id = $1;

-- name: ConsumePatchScheduleSlot :one
-- Atomically claims a schedule slot (compare-and-set on last_run_at) so that
-- concurrent dispatchers cannot run the same slot twice. Returns no row when
-- the slot was already consumed. One-shot schedules are disabled in the same
-- statement so a claim can never leave them armed.
-- updated_at is intentionally left alone: it tracks config changes and the
-- dispatcher skips slots older than it (no instant fire on create/enable).
UPDATE patch_schedules
SET last_run_at = sqlc.arg('last_run_at'),
    enabled = CASE WHEN schedule_type = 'once' THEN false ELSE enabled END
WHERE id = sqlc.arg('id')
  AND enabled = true
  AND (last_run_at IS NULL OR last_run_at < sqlc.arg('slot'))
RETURNING id;

-- name: MarkPatchScheduleMissed :exec
UPDATE patch_schedules SET enabled = false, missed_at = sqlc.arg('missed_at'), updated_at = NOW()
WHERE id = sqlc.arg('id');

-- name: UserCanManagePatching :one
-- Execution-time revalidation of the schedule creator: the schedule must stop
-- firing once its creator is deleted, deactivated or loses can_manage_patching.
SELECT u.is_active AND rp.can_manage_patching AS allowed
FROM users u
JOIN role_permissions rp ON rp.role = u.role
WHERE u.id = $1;

-- name: ListPatchScheduleGroupHosts :many
SELECT h.id, h.api_id, h.friendly_name, h.hostname
FROM host_group_memberships hgm
JOIN hosts h ON h.id = hgm.host_id
WHERE hgm.host_group_id = $1
ORDER BY h.friendly_name;
