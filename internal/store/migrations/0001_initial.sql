-- 0001_initial.sql -- the whole schema of §12.3, installed in one transaction.
--
-- NO PRAGMA IN THIS FILE, EVER. A "PRAGMA journal_mode = WAL" at the top of a
-- script that runs inside a transaction fails with "cannot change into wal mode
-- from within a transaction", and busy_timeout / foreign_keys are PER-CONNECTION
-- settings: applied here they would hold for the migrating connection only, not
-- for the other connections of the pool. Every pragma therefore travels in the
-- DSN (§12.2), and migrate() owns the BEGIN, the COMMIT and the user_version.
--
-- Every table is STRICT (SQLite >= 3.37, supplied by modernc.org/sqlite). The
-- goal is stated in §12.3: make the legacy "VARCHAR(255) for a weight"
-- impossible to write.

CREATE TABLE meta (key TEXT PRIMARY KEY, value TEXT NOT NULL, updated_at TEXT NOT NULL) STRICT;

-- ------------------------------------------------------------------ CATALOG
CREATE TABLE imports (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    occurred_at TEXT NOT NULL, source TEXT NOT NULL,       -- local_drop|webdav|manual
    file_name TEXT NOT NULL, sha256 TEXT NOT NULL, byte_count INTEGER NOT NULL,
    rows_read INTEGER NOT NULL, unreadable_rows INTEGER NOT NULL,
    -- The three outcomes of the qualification (§10.3), counted separately
    -- because they are worded differently on screen. "hidden_products" is gone:
    -- it summed a prepackaged product and a wrong check digit, which means
    -- nothing.
    weighable INTEGER NOT NULL, not_weighable INTEGER NOT NULL,
    anomalies INTEGER NOT NULL, unit_mismatches INTEGER NOT NULL,
    images_decoded INTEGER NOT NULL, images_rejected INTEGER NOT NULL,
                                   -- 181 and 0 on the real file. Two counters, because
                                   -- "no image decoded" on a file that carried some is a
                                   -- symptom, whereas a catalog without images is a normal
                                   -- case (flv_1.csv).
    products_withdrawn INTEGER NOT NULL,                   -- seen at N-1, absent here (§10.9)
    result TEXT NOT NULL CHECK (result IN ('applied','unchanged','rejected','failed')),
    code TEXT NOT NULL DEFAULT '', reason TEXT NOT NULL DEFAULT '',
    duration_ms INTEGER NOT NULL
) STRICT;
-- APPEND-ONLY: NON unique index (important-2). The same content may be dropped
-- every night; that is a normal event, not an anomaly. With UNIQUE(sha256) a
-- perfectly valid unchanged catalog violated the constraint, aborted the
-- transaction, was not acknowledged, was retried, and ended up permanently
-- banned behind a red light.
CREATE INDEX idx_imports_sha         ON imports(sha256);
CREATE INDEX idx_imports_occurred_at ON imports(occurred_at DESC);

CREATE TABLE quarantine (
    sha256 TEXT PRIMARY KEY, failure_count INTEGER NOT NULL DEFAULT 0,
    first_failure_at TEXT NOT NULL, last_failure_at TEXT NOT NULL,
    code TEXT NOT NULL DEFAULT '', reason TEXT NOT NULL DEFAULT ''
) STRICT;

CREATE TABLE categories (
    code TEXT PRIMARY KEY, label TEXT NOT NULL, rank INTEGER NOT NULL DEFAULT 0,
    color TEXT NOT NULL DEFAULT '', visible INTEGER NOT NULL DEFAULT 1 CHECK (visible IN (0,1))
) STRICT;

CREATE TABLE images (
    sha256 TEXT PRIMARY KEY, byte_count INTEGER NOT NULL,
    format TEXT NOT NULL CHECK (format IN ('jpeg','png','gif','bmp')),
                                   -- REAL format, recognized from the header bytes. The
                                   -- legacy application wrote <id>_image.jpg whatever the
                                   -- content: 10 of the 181 images of the real file are
                                   -- PNGs named .jpg (§10.7). The served extension derives
                                   -- from this column, never the other way round.
    width INTEGER NOT NULL, height INTEGER NOT NULL, seen_at TEXT NOT NULL
) STRICT;

CREATE TABLE products (
    id               TEXT    PRIMARY KEY,      -- Odoo id: the PRODUCER's key, unique and
                                   -- stable (355 out of 355 in the real file, ids from 20
                                   -- to 5209, NOT contiguous: never an index). It survives
                                   -- imports (§10.9).
    name             TEXT    NOT NULL,
                                   -- search_name REMOVED: derived value, computed when the
                                   -- JSON catalog is served. A single source of truth, the
                                   -- name.
    reference        TEXT    NOT NULL DEFAULT '' CHECK (length(reference) IN (0,13)),
                                   -- '' is a NORMAL case: 0 product out of 355 in flv.csv,
                                   -- 9 out of 153 in flv_1.csv (§10.3)
    mode             TEXT    NOT NULL CHECK (mode IN ('by_weight','by_unit')),
                                   -- and not 'P'/'U': those two letters existed only
                                   -- because Access stored them. The domain says
                                   -- ByWeight/ByUnit, and this value is DERIVED FROM THE
                                   -- barcode PREFIX, never from the CSV 'unite' column
                                   -- (§10.2): the till reads the prefix only -- nobody ever
                                   -- read 'P'.
    price_suffix     TEXT    NOT NULL DEFAULT '',
                                   -- ' €/kg' | ' € le litre' | ' € l'unité'. That is ALL
                                   -- the CSV 'unite' column drives (kg 328, Unité(s) 18,
                                   -- Litre(s) 9 in flv.csv). A display, never a rule.
    unit_price_cents INTEGER NOT NULL CHECK (unit_price_cents BETWEEN 0 AND 999999),
                                   -- upper bound = MaxUnitPrice (§6.1): this is the 1st of
                                   -- the three enforcements that make the "no overflow"
                                   -- invariant trivially true
    category_code    TEXT    NOT NULL REFERENCES categories(code) ON DELETE RESTRICT,
    qualification    TEXT    NOT NULL CHECK (qualification IN ('weighable','not_weighable','anomaly')),
    reason           TEXT    NOT NULL DEFAULT '',  -- NO_BARCODE, PREPACKAGED_PRODUCT, ...
    csv_line         INTEGER NOT NULL DEFAULT 0,   -- to name the row to fix
    image_sha256     TEXT    REFERENCES images(sha256) ON DELETE SET NULL,
    seen_at          TEXT    NOT NULL,             -- timestamp of the last import that saw it
    withdrawn_at     TEXT,                         -- NULL = present in the catalog (§10.9)
    last_import_id   INTEGER NOT NULL REFERENCES imports(id) ON DELETE RESTRICT
) STRICT;
CREATE INDEX idx_products_grid      ON products(qualification, category_code, name);
CREATE INDEX idx_products_reference ON products(reference);
-- subcategory REMOVED: the CSV has 7 columns and none carries a subcategory. No producer,
--   hence an empty column in every circumstance -- and §14.3 was building a UI on top of it.
--   We had copied even the UNREALIZED intentions of the legacy application.
-- organic REMOVED: derived from a substring of the name -- false on the 153 rows of
--   flv_1.csv, true on 83 of the 355 of flv.csv: a criterion that jumps from 0 % to 23 %
--   between two exports of the same shop. And without a single consumer here (§10.2).
-- visible REMOVED: it was a stored derived column. The grid reads
--   "qualification = 'weighable' AND withdrawn_at IS NULL" joined to local_decisions.
-- rank REMOVED: no path of this document wrote it, and it nevertheless appeared in 3rd
--   position of the grid index. Sorting is alphabetical.
-- anomalies (JSON) REMOVED: it duplicated the findings table below.
-- No index on the unaccented name: the search happens BROWSER-SIDE on the complete catalog
-- (~60 kB for 355 products, images excluded: they are served separately and addressed by
-- their sha). A LIKE '%AIL%' cannot use a B-tree index anyway. Should the catalog ever
-- exceed 5 000 rows: FTS5.

-- HUMAN decision, distinct from the computed qualification (§10.6). Ordinary foreign key:
-- the product no longer disappears from one import to the next (§10.9).
CREATE TABLE local_decisions (
    product_id   TEXT PRIMARY KEY REFERENCES products(id) ON DELETE CASCADE,
    offered      INTEGER NOT NULL DEFAULT 1 CHECK (offered IN (0,1)),
    -- Minimum weight exemption, PER PRODUCT (§10.6). NULL = limits.min_weight_g.
    -- Replaces limits.light_product_terms: no runtime rule depends any more on a substring
    -- of a name that Odoo may rename without notice.
    min_weight_g INTEGER CHECK (min_weight_g IS NULL OR min_weight_g > 0),
    reason TEXT NOT NULL DEFAULT '', decided_at TEXT NOT NULL, decided_by TEXT NOT NULL DEFAULT ''
) STRICT;

CREATE TABLE findings (
    import_id INTEGER NOT NULL REFERENCES imports(id) ON DELETE CASCADE,
    csv_line INTEGER NOT NULL, product_id TEXT,
    code TEXT NOT NULL,                        -- INVALID_BARCODE, UNIT_MISMATCH, ...
    issue TEXT NOT NULL CHECK (issue IN ('anomaly','info')),
    message TEXT NOT NULL, value TEXT NOT NULL DEFAULT ''
) STRICT;
CREATE INDEX idx_findings_import ON findings(import_id, issue);
-- "blocking 0/1" became "issue": a finding BLOCKS nothing, it SAYS something. What decides
-- whether a product enters the grid is its qualification, and that is carried by the
-- product itself.

-- -------------------------------------------------------------------- JOURNAL
CREATE TABLE weighings (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    occurred_at TEXT NOT NULL, station INTEGER NOT NULL,
    job_id TEXT NOT NULL UNIQUE,               -- ULID -- absolute duplicate guard
    idempotency_key TEXT NOT NULL DEFAULT '',
    product_id TEXT REFERENCES products(id) ON DELETE RESTRICT,  -- REAL foreign key (§10.9)
    product_name TEXT NOT NULL,                -- display snapshot: the name moves in Odoo,
                                               -- the journal row does not
    reference TEXT NOT NULL,
    mode TEXT NOT NULL CHECK (mode IN ('by_weight','by_unit')),
    gross_weight_g INTEGER NOT NULL DEFAULT 0,
    tare_g INTEGER NOT NULL DEFAULT 0,
    net_weight_g INTEGER NOT NULL DEFAULT 0,
    quantity INTEGER NOT NULL DEFAULT 0,
    base_unit_price_cents INTEGER NOT NULL DEFAULT 0,
    barcode TEXT NOT NULL DEFAULT '',          -- the one ACTUALLY printed
    source TEXT NOT NULL CHECK (source IN ('scale','manual','replay')),
    stability TEXT NOT NULL CHECK (stability IN ('stable','unstable','unknown','not_applicable')),
    rate_ms INTEGER NOT NULL DEFAULT 0,        -- median rate at the time of the weighing (A3)
    frame TEXT NOT NULL DEFAULT '',            -- living corpus for the replay driver
    result TEXT NOT NULL CHECK (result IN ('sent','rejected','failed','reprint')),
    detail TEXT NOT NULL DEFAULT '',
    duration_ms INTEGER NOT NULL DEFAULT 0
) STRICT;
CREATE INDEX idx_weighings_occurred_at ON weighings(occurred_at DESC);
CREATE INDEX idx_weighings_result      ON weighings(result, occurred_at DESC);
CREATE INDEX idx_weighings_product     ON weighings(product_id, occurred_at DESC);
-- result no longer carries 'ok': the "printed" / "sent" distinction is gone
-- (important-7). A successful weighing is 'sent'.

CREATE TABLE weighing_lines (
    weighing_id INTEGER NOT NULL REFERENCES weighings(id) ON DELETE CASCADE,
    tier_code TEXT NOT NULL,
    unit_price_cents INTEGER NOT NULL, amount_cents INTEGER NOT NULL,
    PRIMARY KEY (weighing_id, tier_code)
) STRICT, WITHOUT ROWID;

-- ------------------------------------------------------------- TECHNICAL LOG
CREATE TABLE technical_log (
    id INTEGER PRIMARY KEY AUTOINCREMENT, occurred_at TEXT NOT NULL,
    level TEXT NOT NULL CHECK (level IN ('debug','info','warn','error','critical')),
    source TEXT NOT NULL CHECK (source IN ('scale','printer','catalog','ui','config','http','system')),
    code TEXT NOT NULL DEFAULT '', message TEXT NOT NULL, detail TEXT NOT NULL DEFAULT ''
) STRICT;
CREATE INDEX idx_technical_log_occurred_at ON technical_log(occurred_at DESC);
CREATE INDEX idx_technical_log_code        ON technical_log(code, occurred_at DESC);
