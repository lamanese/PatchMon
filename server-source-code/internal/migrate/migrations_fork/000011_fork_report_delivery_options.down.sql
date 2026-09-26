ALTER TABLE fork_report_archive DROP COLUMN IF EXISTS delivery_enabled;
ALTER TABLE scheduled_reports DROP CONSTRAINT IF EXISTS fork_archive_keep_range;
ALTER TABLE scheduled_reports
    DROP COLUMN IF EXISTS fork_archive_keep,
    DROP COLUMN IF EXISTS fork_deliver;
