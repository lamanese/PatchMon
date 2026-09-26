-- Report delivery options: a report can be rendered and archived without
-- being sent (fork_deliver = false), and the number of archived runs kept
-- per report is configurable (fork_archive_keep, 1..200, default 24). The
-- archive records whether delivery was on for each run.
ALTER TABLE scheduled_reports
    ADD COLUMN IF NOT EXISTS fork_deliver BOOLEAN NOT NULL DEFAULT true,
    ADD COLUMN IF NOT EXISTS fork_archive_keep INTEGER NOT NULL DEFAULT 24;
ALTER TABLE scheduled_reports DROP CONSTRAINT IF EXISTS fork_archive_keep_range;
ALTER TABLE scheduled_reports
    ADD CONSTRAINT fork_archive_keep_range CHECK (fork_archive_keep BETWEEN 1 AND 200);
ALTER TABLE fork_report_archive
    ADD COLUMN IF NOT EXISTS delivery_enabled BOOLEAN NOT NULL DEFAULT true;
