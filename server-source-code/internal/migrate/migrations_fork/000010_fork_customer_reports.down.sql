DROP TABLE IF EXISTS fork_report_deliveries;
DROP TABLE IF EXISTS fork_report_archive;
ALTER TABLE scheduled_reports
    DROP COLUMN IF EXISTS fork_email_recipients;
