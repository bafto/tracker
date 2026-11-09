-- name: ListActivities :many
SELECT * FROM activity ORDER BY sort_order, id;

-- name: GetActivity :one
SELECT * FROM activity WHERE id = ?;

-- name: CreateActivity :one
INSERT INTO activity (name, unit, color, sort_order, created_at)
VALUES (?, ?, ?, (SELECT COALESCE(MAX(sort_order), 0) + 1 FROM activity), ?)
RETURNING id;

-- name: UpdateActivity :execrows
UPDATE activity SET name = ?, unit = ?, color = ? WHERE id = ?;

-- name: SetActivityArchived :execrows
UPDATE activity SET archived_at = ? WHERE id = ?;

-- name: SetActivitySortOrder :exec
UPDATE activity SET sort_order = ? WHERE id = ?;

-- name: ListSchedules :many
SELECT * FROM activity_schedule ORDER BY activity_id, effective_from;

-- name: ListSchedulesForActivity :many
SELECT * FROM activity_schedule WHERE activity_id = ? ORDER BY effective_from;

-- name: UpsertSchedule :exec
INSERT INTO activity_schedule (activity_id, effective_from, kind, weekday_mask, times_per_week)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT (activity_id, effective_from) DO UPDATE SET
	kind = excluded.kind,
	weekday_mask = excluded.weekday_mask,
	times_per_week = excluded.times_per_week;

-- name: DeleteSchedulesForActivity :exec
DELETE FROM activity_schedule WHERE activity_id = ?;
