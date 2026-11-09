-- name: ListLogsInRange :many
SELECT * FROM log_entry
WHERE day >= sqlc.arg(from_day) AND day <= sqlc.arg(to_day)
ORDER BY day, activity_id;

-- name: ListAllLogs :many
SELECT * FROM log_entry ORDER BY day, activity_id;

-- name: CountLogsBefore :one
SELECT COUNT(*) FROM log_entry WHERE activity_id = ? AND day < ?;

-- name: GetLog :one
SELECT * FROM log_entry WHERE activity_id = ? AND day = ?;

-- name: UpsertLog :exec
INSERT INTO log_entry (activity_id, day, amount, note)
VALUES (?, ?, ?, ?)
ON CONFLICT (activity_id, day) DO UPDATE SET
	amount = excluded.amount,
	note = excluded.note,
	updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now');

-- name: DeleteLog :execrows
DELETE FROM log_entry WHERE activity_id = ? AND day = ?;

-- name: ListDayNotesInRange :many
SELECT * FROM day_note
WHERE day >= sqlc.arg(from_day) AND day <= sqlc.arg(to_day)
ORDER BY day;

-- name: ListAllDayNotes :many
SELECT * FROM day_note ORDER BY day;

-- name: UpsertDayNote :exec
INSERT INTO day_note (day, note) VALUES (?, ?)
ON CONFLICT (day) DO UPDATE SET note = excluded.note;

-- name: DeleteDayNote :exec
DELETE FROM day_note WHERE day = ?;
