-- Customer reports (increment D): recipients per report, an archive of every
-- run with the rendered output and one row per delivery. Lists never read the
-- blob columns (pdf, html, csv); the worker keeps the 24 newest rows per report.
ALTER TABLE scheduled_reports
    ADD COLUMN IF NOT EXISTS fork_email_recipients TEXT[];

CREATE TABLE IF NOT EXISTS fork_report_archive (
    id                  TEXT PRIMARY KEY,
    scheduled_report_id TEXT NOT NULL REFERENCES scheduled_reports(id) ON DELETE CASCADE,
    run_key             TEXT NOT NULL UNIQUE,
    trigger_kind        TEXT NOT NULL CHECK (trigger_kind IN ('scheduled', 'manual')),
    slot_at             TIMESTAMPTZ,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    finished_at         TIMESTAMPTZ,
    status              TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'completed', 'partial', 'failed')),
    error_code          TEXT,
    error_message       TEXT,
    report_name         TEXT NOT NULL,
    language            TEXT NOT NULL DEFAULT 'en',
    period_from         TIMESTAMPTZ,
    period_to           TIMESTAMPTZ,
    group_ids           TEXT[] NOT NULL DEFAULT '{}',
    group_names         TEXT[] NOT NULL DEFAULT '{}',
    host_count          INTEGER NOT NULL DEFAULT 0,
    customer_mode       BOOLEAN NOT NULL DEFAULT false,
    smtp_destination_id TEXT,
    mail_from           TEXT,
    recipients          TEXT[] NOT NULL DEFAULT '{}',
    subject             TEXT NOT NULL DEFAULT '',
    html                TEXT,
    csv                 TEXT,
    pdf                 BYTEA,
    pdf_size            INTEGER NOT NULL DEFAULT 0,
    pdf_sha256          TEXT
);
CREATE INDEX IF NOT EXISTS fork_report_archive_report_created_idx
    ON fork_report_archive (scheduled_report_id, created_at DESC);

CREATE TABLE IF NOT EXISTS fork_report_deliveries (
    id               TEXT PRIMARY KEY,
    archive_id       TEXT NOT NULL REFERENCES fork_report_archive(id) ON DELETE CASCADE,
    destination_id   TEXT NOT NULL,
    destination_name TEXT NOT NULL DEFAULT '',
    channel          TEXT NOT NULL,
    recipient        TEXT NOT NULL DEFAULT '',
    status           TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'sent', 'failed')),
    error_code       TEXT,
    error_message    TEXT,
    attempts         INTEGER NOT NULL DEFAULT 0,
    sent_at          TIMESTAMPTZ,
    UNIQUE (archive_id, destination_id, recipient)
);
