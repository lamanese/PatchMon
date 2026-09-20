-- fork_is_definition_update: classifies a host_packages row as a Windows
-- Defender "Security Intelligence Update" / definition update (KB2267602),
-- which Microsoft republishes several times a day and which WUA reports as
-- a brand-new pending update every time (PM_IGNORE_DEFINITION_UPDATES).
--
-- WUA's Category.Name strings are localised by the OS UI language, so a
-- literal 'Definition Updates' match only works on English hosts. This
-- function matches on the localised category names PatchMon has observed
-- (en/de/fr/it) OR on the well-known KB number, which is language-neutral.
-- wua_kb is stored "KB<digits>" (see agent-source-code
-- internal/packages/windows.go and server-source-code internal/store/report.go),
-- possibly as a comma-separated list when WUA associates more than one KB
-- with an update, so the KB match splits on commas and tolerates a value
-- with or without the "KB" prefix.
--
-- WUA also exposes a stable, language-independent CategoryID GUID
-- (e0789628-ce08-4437-be74-2495b842f43b for "Definition Updates") but the
-- agent does not report CategoryIDs yet - only Category.Name strings. When
-- it does, only this function needs to change; every query that calls it
-- stays the same.
--
-- NULL-safe: returns false (never NULL) for NULL/missing categories and
-- NULL kb, and false (never an error) when categories is present but is not
-- a JSON array (e.g. a stray JSON string or object).
CREATE OR REPLACE FUNCTION fork_is_definition_update(categories jsonb, kb text)
RETURNS boolean
LANGUAGE sql
IMMUTABLE
PARALLEL SAFE
AS $$
    SELECT COALESCE(
        jsonb_typeof(categories) = 'array'
        AND categories ?| ARRAY[
            'Definition Updates',                  -- en
            'Definitionsupdates',                  -- de
            'Mises à jour de définitions',          -- fr
            'Aggiornamenti delle definizioni'       -- it
        ],
        false
    )
    OR COALESCE(
        kb IS NOT NULL
        AND EXISTS (
            SELECT 1
            FROM regexp_split_to_table(upper(kb), '\s*,\s*') AS t(tok)
            WHERE regexp_replace(t.tok, '^KB', '') = '2267602'
        ),
        false
    );
$$;
