-- Fork: customer reports (increment D). Slot claim on scheduled_reports,
-- recipients, run archive and per-delivery status. List queries never read
-- pdf/html/csv. All timestamps on the fork tables are TIMESTAMPTZ (Go
-- time.Time / *time.Time via the sqlc override); scheduled_reports keeps the
-- upstream TIMESTAMP(3) columns (pgtype.Timestamp via pgtime.From).

-- name: ForkClaimScheduledReportSlot :execrows
-- Atomic slot claim: succeeds only while next_run_at still points at the slot
-- the task was enqueued for. Compared at second precision because
-- notifications.NextCronRun yields whole seconds and legacy rows may carry ms.
UPDATE scheduled_reports
SET last_run_at = sqlc.arg('now'), next_run_at = sqlc.arg('next'), updated_at = NOW()
WHERE id = sqlc.arg('id') AND enabled = true
  AND date_trunc('second', next_run_at) = date_trunc('second', sqlc.arg('slot')::timestamp);

-- name: ForkSetScheduledReportNextRunIfNull :execrows
UPDATE scheduled_reports SET next_run_at = sqlc.arg('next'), updated_at = NOW()
WHERE id = sqlc.arg('id') AND next_run_at IS NULL;

-- name: ForkSetScheduledReportRecipients :exec
UPDATE scheduled_reports SET fork_email_recipients = sqlc.narg('recipients')::text[], updated_at = NOW()
WHERE id = sqlc.arg('id');

-- name: ForkInsertReportArchive :exec
INSERT INTO fork_report_archive (id, scheduled_report_id, run_key, trigger_kind, slot_at, report_name, language, customer_mode, group_ids, recipients)
VALUES (sqlc.arg('id'), sqlc.arg('scheduled_report_id'), sqlc.arg('run_key'), sqlc.arg('trigger_kind'), sqlc.narg('slot_at'),
        sqlc.arg('report_name'), sqlc.arg('language'), sqlc.arg('customer_mode'),
        COALESCE(sqlc.arg('group_ids')::text[], '{}'::text[]), COALESCE(sqlc.arg('recipients')::text[], '{}'::text[]));

-- name: ForkGetReportArchiveByRunKey :one
SELECT id, scheduled_report_id, run_key, trigger_kind, slot_at, created_at, finished_at, status, error_code, error_message,
       report_name, language, period_from, period_to, group_ids, group_names, host_count, customer_mode,
       smtp_destination_id, mail_from, recipients, pdf_size, pdf_sha256, (pdf IS NOT NULL)::boolean AS has_pdf
FROM fork_report_archive WHERE run_key = $1;

-- name: ForkSnapshotReportArchive :exec
UPDATE fork_report_archive
SET period_from = sqlc.arg('period_from'), period_to = sqlc.arg('period_to'),
    group_ids = COALESCE(sqlc.arg('group_ids')::text[], '{}'::text[]),
    group_names = COALESCE(sqlc.arg('group_names')::text[], '{}'::text[]), host_count = sqlc.arg('host_count'),
    smtp_destination_id = sqlc.narg('smtp_destination_id'), mail_from = sqlc.narg('mail_from'),
    subject = sqlc.arg('subject'), html = sqlc.arg('html'), csv = sqlc.arg('csv'),
    pdf = sqlc.arg('pdf'), pdf_size = sqlc.arg('pdf_size'), pdf_sha256 = sqlc.arg('pdf_sha256')
WHERE id = sqlc.arg('id');

-- name: ForkGetReportArchiveContent :one
SELECT id, scheduled_report_id, status, report_name, language, customer_mode, smtp_destination_id, mail_from, recipients,
       subject, COALESCE(html, '')::text AS html, COALESCE(csv, '')::text AS csv, pdf, pdf_sha256, created_at
FROM fork_report_archive WHERE id = $1;

-- name: ForkFinishReportArchive :exec
UPDATE fork_report_archive
SET status = sqlc.arg('status'), error_code = sqlc.narg('error_code'), error_message = sqlc.narg('error_message'), finished_at = NOW()
WHERE id = sqlc.arg('id');

-- name: ForkListReportArchive :many
SELECT id, scheduled_report_id, run_key, trigger_kind, slot_at, created_at, finished_at, status, error_code, error_message,
       report_name, language, period_from, period_to, group_ids, group_names, host_count, customer_mode,
       smtp_destination_id, mail_from, recipients, pdf_size, pdf_sha256, (pdf IS NOT NULL)::boolean AS has_pdf
FROM fork_report_archive
WHERE scheduled_report_id = sqlc.arg('scheduled_report_id')
ORDER BY created_at DESC, id
LIMIT sqlc.arg('limit');

-- name: ForkListReportDeliveries :many
SELECT * FROM fork_report_deliveries WHERE archive_id = $1 ORDER BY channel, destination_id, recipient, id;

-- name: ForkListReportDeliveriesForReport :many
SELECT d.* FROM fork_report_deliveries d
JOIN fork_report_archive a ON a.id = d.archive_id
WHERE a.scheduled_report_id = $1
ORDER BY a.created_at DESC, d.channel, d.destination_id, d.recipient, d.id;

-- name: ForkInsertReportDelivery :exec
INSERT INTO fork_report_deliveries (id, archive_id, destination_id, destination_name, channel, recipient)
VALUES (sqlc.arg('id'), sqlc.arg('archive_id'), sqlc.arg('destination_id'), sqlc.arg('destination_name'), sqlc.arg('channel'), sqlc.arg('recipient'))
ON CONFLICT (archive_id, destination_id, recipient) DO NOTHING;

-- name: ForkMarkReportDelivery :exec
UPDATE fork_report_deliveries
SET status = sqlc.arg('status'), error_code = sqlc.narg('error_code'), error_message = sqlc.narg('error_message'),
    attempts = attempts + 1,
    sent_at = CASE WHEN sqlc.arg('status')::text = 'sent' THEN NOW() ELSE sent_at END
WHERE id = sqlc.arg('id');

-- name: ForkGetReportArchivePDF :one
SELECT a.id, a.scheduled_report_id, a.report_name, a.created_at, a.status, a.pdf, s.timezone
FROM fork_report_archive a JOIN scheduled_reports s ON s.id = a.scheduled_report_id
WHERE a.id = $1 AND a.pdf IS NOT NULL;

-- name: ForkAbandonStaleReportArchive :execrows
UPDATE fork_report_archive
SET status = 'failed', error_code = 'abandoned', error_message = 'run did not finish within 24 hours', finished_at = NOW()
WHERE scheduled_report_id = $1 AND status = 'pending' AND created_at < NOW() - INTERVAL '24 hours';

-- name: ForkPruneReportArchive :execrows
DELETE FROM fork_report_archive AS fra
WHERE fra.scheduled_report_id = sqlc.arg('id')
  AND fra.id NOT IN (
      SELECT id FROM fork_report_archive
      WHERE scheduled_report_id = sqlc.arg('id')
      ORDER BY created_at DESC, id
      LIMIT sqlc.arg('keep')
  )
  AND NOT (fra.status = 'pending' AND fra.created_at > NOW() - INTERVAL '24 hours');
