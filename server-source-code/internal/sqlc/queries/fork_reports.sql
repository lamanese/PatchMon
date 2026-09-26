-- Fork: queries for host-group-scoped scheduled reports (internal/reports).
-- Every query filters by host_ids FIRST and only then aggregates, sorts or
-- limits, so a report for one group can never see another group's data.
-- Security/update counts use the same definition-update exclusion as the
-- dashboard (fork_is_definition_update + PM_IGNORE_DEFINITION_UPDATES).

-- name: ForkReportAllHostIDs :many
SELECT id FROM hosts ORDER BY friendly_name, id;

-- name: ForkReportGroupsByIDs :many
SELECT id, name FROM host_groups WHERE id = ANY(sqlc.arg('group_ids')::text[]) ORDER BY name, id;

-- name: ForkReportHosts :many
-- One row per scope host with update counters and the latest real patch run.
SELECT h.id, h.friendly_name, h.hostname, h.api_id, h.os_type, h.os_version, h.status,
       h.last_update, h.agent_version, h.needs_reboot, h.fork_boot_time, h.system_uptime,
       h.disk_details, h.fork_pkg_broken,
       COALESCE(uc.cnt, 0)::int AS updates_count,
       COALESCE(sc.cnt, 0)::int AS security_updates_count,
       COALESCE(lp.id, '')::text AS last_run_id, COALESCE(lp.status, '')::text AS last_run_status,
       lp.created_at AS last_run_created_at, lp.completed_at AS last_run_completed_at
FROM hosts h
LEFT JOIN (
    SELECT host_id, COUNT(*) AS cnt FROM host_packages
    WHERE host_id = ANY(sqlc.arg('host_ids')::text[]) AND needs_update = true
      AND NOT (sqlc.arg('ignore_definition_updates')::boolean AND fork_is_definition_update(wua_categories, wua_kb))
    GROUP BY host_id
) uc ON uc.host_id = h.id
LEFT JOIN (
    SELECT host_id, COUNT(*) AS cnt FROM host_packages
    WHERE host_id = ANY(sqlc.arg('host_ids')::text[]) AND needs_update = true AND is_security_update = true
      AND NOT (sqlc.arg('ignore_definition_updates')::boolean AND fork_is_definition_update(wua_categories, wua_kb))
    GROUP BY host_id
) sc ON sc.host_id = h.id
LEFT JOIN LATERAL (
    SELECT pr.id, pr.status, pr.created_at, pr.completed_at FROM patch_runs pr
    WHERE pr.host_id = h.id AND pr.dry_run = false ORDER BY pr.created_at DESC, pr.id LIMIT 1
) lp ON true
WHERE h.id = ANY(sqlc.arg('host_ids')::text[])
ORDER BY h.friendly_name, h.id;

-- name: ForkReportPatchRunStats :many
SELECT status, COUNT(*)::int AS cnt FROM patch_runs
WHERE host_id = ANY(sqlc.arg('host_ids')::text[]) AND dry_run = false
  AND created_at >= sqlc.arg('period_from')::timestamp AND created_at < sqlc.arg('period_to')::timestamp
GROUP BY status ORDER BY status;

-- name: ForkReportRecentPatchRuns :many
SELECT pr.id, pr.host_id, h.friendly_name, h.hostname, pr.status, pr.patch_type, pr.package_name, pr.package_names,
       pr.created_at, pr.started_at, pr.completed_at
FROM patch_runs pr JOIN hosts h ON h.id = pr.host_id
WHERE pr.host_id = ANY(sqlc.arg('host_ids')::text[]) AND pr.dry_run = false
ORDER BY pr.created_at DESC, pr.id LIMIT sqlc.arg('max_rows')::int;

-- name: ForkReportPatchActivity :many
SELECT pr.id, pr.host_id, h.friendly_name, pr.status, pr.patch_type, pr.dry_run, pr.packages_affected,
       pr.created_at, pr.completed_at
FROM patch_runs pr JOIN hosts h ON h.id = pr.host_id
WHERE pr.host_id = ANY(sqlc.arg('host_ids')::text[])
  AND pr.created_at >= sqlc.arg('period_from')::timestamp AND pr.created_at < sqlc.arg('period_to')::timestamp
ORDER BY pr.created_at DESC, pr.id LIMIT sqlc.arg('max_rows')::int;

-- name: ForkReportComplianceLatest :many
-- Latest completed scan per host AND profile inside the scope.
SELECT cs.host_id, h.friendly_name, cs.profile_id, cp.name AS profile_name, cs.score, cs.passed, cs.failed, cs.completed_at
FROM (
    SELECT DISTINCT ON (host_id, profile_id) id, host_id, profile_id, score, passed, failed, completed_at
    FROM compliance_scans
    WHERE status = 'completed' AND host_id = ANY(sqlc.arg('host_ids')::text[])
    ORDER BY host_id, profile_id, completed_at DESC NULLS LAST
) cs
JOIN hosts h ON h.id = cs.host_id
JOIN compliance_profiles cp ON cp.id = cs.profile_id
ORDER BY cs.score ASC NULLS FIRST, h.friendly_name, cp.name;

-- name: ForkReportOpenAlerts :many
-- Host alerts carry the host only in metadata.host_id; global alerts have none.
SELECT a.id, a.type, a.severity, a.title, a.created_at,
       COALESCE(a.metadata->>'host_id', '')::text AS host_id,
       h.friendly_name AS host_name
FROM alerts a LEFT JOIN hosts h ON h.id = a.metadata->>'host_id'
WHERE a.is_active = true AND a.resolved_at IS NULL
  AND ((a.metadata->>'host_id') = ANY(sqlc.arg('host_ids')::text[])
       OR (sqlc.arg('include_global')::boolean AND (a.metadata->>'host_id') IS NULL))
ORDER BY a.created_at DESC, a.id;

-- name: ForkReportSecurityUpdates :many
-- Available version ONLY from host_packages; packages.latest_version is a
-- shared catalog that any host may overwrite.
SELECT hp.host_id, h.friendly_name, p.name AS package_name, hp.current_version, hp.available_version
FROM host_packages hp JOIN packages p ON p.id = hp.package_id JOIN hosts h ON h.id = hp.host_id
WHERE hp.host_id = ANY(sqlc.arg('host_ids')::text[])
  AND hp.needs_update = true AND hp.is_security_update = true
  AND NOT (sqlc.arg('ignore_definition_updates')::boolean AND fork_is_definition_update(hp.wua_categories, hp.wua_kb))
ORDER BY h.friendly_name, hp.host_id, p.name;

-- name: ForkReportReboots :many
SELECT jh.id, jh.host_id, h.friendly_name, jh.status, jh.error_message, jh.created_at, jh.completed_at
FROM job_history jh LEFT JOIN hosts h ON h.id = jh.host_id
WHERE jh.job_name = 'reboot' AND jh.host_id = ANY(sqlc.arg('host_ids')::text[])
  AND jh.created_at >= sqlc.arg('period_from')::timestamp AND jh.created_at < sqlc.arg('period_to')::timestamp
ORDER BY jh.created_at DESC, jh.id LIMIT sqlc.arg('max_rows')::int;
