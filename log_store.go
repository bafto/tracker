package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/bafto/tracker/db"
)

func mapLog(l db.LogEntry) LogEntry {
	day, _ := parseDay(l.Day)
	return LogEntry{ActivityID: l.ActivityID, Day: day, Amount: l.Amount, Note: l.Note}
}

func groupLogs(rows []db.LogEntry) map[int64]ActivityLogs {
	out := map[int64]ActivityLogs{}
	for _, r := range rows {
		e := mapLog(r)
		if out[e.ActivityID] == nil {
			out[e.ActivityID] = ActivityLogs{}
		}
		out[e.ActivityID][e.Day] = e
	}
	return out
}

// LogsInRange returns logs in the inclusive range, grouped by activity.
func (s *DBStore) LogsInRange(ctx context.Context, from, to time.Time) (map[int64]ActivityLogs, error) {
	rows, err := s.getQueries(ctx).ListLogsInRange(ctx, db.ListLogsInRangeParams{FromDay: fmtDay(from), ToDay: fmtDay(to)})
	if err != nil {
		return nil, fmt.Errorf("list logs: %w", err)
	}
	return groupLogs(rows), nil
}

func (s *DBStore) AllLogs(ctx context.Context) (map[int64]ActivityLogs, error) {
	rows, err := s.getQueries(ctx).ListAllLogs(ctx)
	if err != nil {
		return nil, fmt.Errorf("list logs: %w", err)
	}
	return groupLogs(rows), nil
}

// GetLog returns the entry, or nil if the activity wasn't done that day.
func (s *DBStore) GetLog(ctx context.Context, activityID int64, day time.Time) (*LogEntry, error) {
	row, err := s.getQueries(ctx).GetLog(ctx, db.GetLogParams{ActivityID: activityID, Day: fmtDay(day)})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	} else if err != nil {
		return nil, fmt.Errorf("get log: %w", err)
	}
	e := mapLog(row)
	return &e, nil
}

// ToggleLog marks the activity done on day, or removes the entry (incl.
// amount and note) if it was already done.
func (s *DBStore) ToggleLog(ctx context.Context, activityID int64, day time.Time) error {
	return s.withinTransaction(ctx, func(ctx context.Context) error {
		q := s.getQueries(ctx)
		n, err := q.DeleteLog(ctx, db.DeleteLogParams{ActivityID: activityID, Day: fmtDay(day)})
		if err != nil {
			return fmt.Errorf("delete log: %w", err)
		}
		if n > 0 {
			return nil
		}
		return s.SaveLog(ctx, activityID, day, nil, "")
	})
}

func (s *DBStore) SaveLog(ctx context.Context, activityID int64, day time.Time, amount *float64, note string) error {
	if err := s.getQueries(ctx).UpsertLog(ctx, db.UpsertLogParams{ActivityID: activityID, Day: fmtDay(day), Amount: amount, Note: note}); err != nil {
		return fmt.Errorf("upsert log: %w", err)
	}
	return nil
}

func (s *DBStore) DayNotesInRange(ctx context.Context, from, to time.Time) (map[time.Time]string, error) {
	rows, err := s.getQueries(ctx).ListDayNotesInRange(ctx, db.ListDayNotesInRangeParams{FromDay: fmtDay(from), ToDay: fmtDay(to)})
	if err != nil {
		return nil, fmt.Errorf("list day notes: %w", err)
	}
	out := map[time.Time]string{}
	for _, r := range rows {
		d, _ := parseDay(r.Day)
		out[d] = r.Note
	}
	return out, nil
}

func (s *DBStore) SetDayNote(ctx context.Context, day time.Time, note string) error {
	q := s.getQueries(ctx)
	if note == "" {
		return q.DeleteDayNote(ctx, fmtDay(day))
	}
	return q.UpsertDayNote(ctx, db.UpsertDayNoteParams{Day: fmtDay(day), Note: note})
}
