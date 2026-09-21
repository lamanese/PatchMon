ALTER TABLE hosts
    DROP COLUMN IF EXISTS fork_pkg_broken,
    DROP COLUMN IF EXISTS fork_pkg_broken_detail;
