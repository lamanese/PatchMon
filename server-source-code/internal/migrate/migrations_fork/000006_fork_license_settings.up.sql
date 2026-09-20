-- License / host limit settings (fork feature).
-- license_max_hosts NULL = unlicensed/unlimited (no behaviour change for
-- existing installations). license_enforce=false = banner only.
ALTER TABLE settings ADD COLUMN IF NOT EXISTS license_max_hosts INTEGER;
ALTER TABLE settings ADD COLUMN IF NOT EXISTS license_enforce BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE settings ADD COLUMN IF NOT EXISTS license_package TEXT;
