CREATE TABLE activity (
	id          INTEGER PRIMARY KEY,
	name        TEXT    NOT NULL,
	unit        TEXT    NOT NULL DEFAULT '',  -- '' = plain checkbox, no amount
	color       TEXT    NOT NULL,
	sort_order  INTEGER NOT NULL,
	archived_at TEXT,                         -- RFC3339, NULL = active
	created_at  TEXT    NOT NULL              -- 'YYYY-MM-DD'
);

-- versioned schedule: the row in effect on day D is the one with the
-- greatest effective_from <= D
CREATE TABLE activity_schedule (
	id             INTEGER PRIMARY KEY,
	activity_id    INTEGER NOT NULL REFERENCES activity(id) ON DELETE CASCADE,
	effective_from TEXT    NOT NULL, -- 'YYYY-MM-DD'
	kind           TEXT    NOT NULL CHECK (kind IN ('weekdays', 'weekly')),
	weekday_mask   INTEGER NOT NULL DEFAULT 0, -- bit0 = Monday ... bit6 = Sunday
	times_per_week INTEGER NOT NULL DEFAULT 0,
	UNIQUE (activity_id, effective_from)
);

-- one row per activity per day = "done"
CREATE TABLE log_entry (
	activity_id INTEGER NOT NULL REFERENCES activity(id) ON DELETE CASCADE,
	day         TEXT    NOT NULL, -- 'YYYY-MM-DD'
	amount      REAL,
	note        TEXT    NOT NULL DEFAULT '',
	created_at  TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
	updated_at  TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
	PRIMARY KEY (activity_id, day)
);

CREATE INDEX log_entry_day ON log_entry(day);

CREATE TABLE day_note (
	day  TEXT PRIMARY KEY, -- 'YYYY-MM-DD'
	note TEXT NOT NULL
);

