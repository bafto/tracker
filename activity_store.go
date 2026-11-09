package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/bafto/tracker/db"
)

var errNotFound = errors.New("not found")

func mapSchedule(s db.ActivitySchedule) Schedule {
	from, _ := parseDay(s.EffectiveFrom)
	return Schedule{
		EffectiveFrom: from,
		Kind:          ScheduleKind(s.Kind),
		WeekdayMask:   uint8(s.WeekdayMask),
		TimesPerWeek:  int(s.TimesPerWeek),
	}
}

func mapActivity(a db.Activity, schedules []Schedule) Activity {
	created, _ := parseDay(a.CreatedAt)
	return Activity{
		ID:        a.ID,
		Name:      a.Name,
		Unit:      a.Unit,
		Color:     a.Color,
		SortOrder: a.SortOrder,
		Archived:  a.ArchivedAt != nil,
		CreatedAt: created,
		Schedules: schedules,
	}
}

// ListActivities returns all activities ordered by sort order, including
// their schedule history. Archived ones are included only if requested.
func (s *DBStore) ListActivities(ctx context.Context, includeArchived bool) ([]Activity, error) {
	q := s.getQueries(ctx)
	rows, err := q.ListActivities(ctx)
	if err != nil {
		return nil, fmt.Errorf("list activities: %w", err)
	}
	schedRows, err := q.ListSchedules(ctx)
	if err != nil {
		return nil, fmt.Errorf("list schedules: %w", err)
	}
	schedules := map[int64][]Schedule{}
	for _, r := range schedRows {
		schedules[r.ActivityID] = append(schedules[r.ActivityID], mapSchedule(r))
	}

	activities := make([]Activity, 0, len(rows))
	for _, r := range rows {
		if r.ArchivedAt != nil && !includeArchived {
			continue
		}
		activities = append(activities, mapActivity(r, schedules[r.ID]))
	}
	return activities, nil
}

func (s *DBStore) GetActivity(ctx context.Context, id int64) (Activity, error) {
	q := s.getQueries(ctx)
	row, err := q.GetActivity(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return Activity{}, errNotFound
	} else if err != nil {
		return Activity{}, fmt.Errorf("get activity: %w", err)
	}
	schedRows, err := q.ListSchedulesForActivity(ctx, id)
	if err != nil {
		return Activity{}, fmt.Errorf("list schedules: %w", err)
	}
	return mapActivity(row, mapSlice(schedRows, mapSchedule)), nil
}

func (s *DBStore) upsertSchedule(ctx context.Context, activityID int64, from string, sched Schedule) error {
	return s.getQueries(ctx).UpsertSchedule(ctx, db.UpsertScheduleParams{
		ActivityID:    activityID,
		EffectiveFrom: from,
		Kind:          string(sched.Kind),
		WeekdayMask:   int64(sched.WeekdayMask),
		TimesPerWeek:  int64(sched.TimesPerWeek),
	})
}

func (s *DBStore) CreateActivity(ctx context.Context, a Activity, sched Schedule, now time.Time) (int64, error) {
	var id int64
	err := s.withinTransaction(ctx, func(ctx context.Context) error {
		var err error
		id, err = s.getQueries(ctx).CreateActivity(ctx, db.CreateActivityParams{
			Name:      a.Name,
			Unit:      a.Unit,
			Color:     a.Color,
			CreatedAt: fmtDay(now),
		})
		if err != nil {
			return fmt.Errorf("create activity: %w", err)
		}
		return s.upsertSchedule(ctx, id, sinceForever, sched)
	})
	return id, err
}

// UpdateActivity updates name/unit/color and, if the schedule changed,
// records a new schedule version effective today. If nothing was logged
// before today, the history is replaced instead (fixing a fresh mistake
// shouldn't leave a stale version behind).
func (s *DBStore) UpdateActivity(ctx context.Context, a Activity, sched Schedule, now time.Time) error {
	return s.withinTransaction(ctx, func(ctx context.Context) error {
		q := s.getQueries(ctx)
		n, err := q.UpdateActivity(ctx, db.UpdateActivityParams{Name: a.Name, Unit: a.Unit, Color: a.Color, ID: a.ID})
		if err != nil {
			return fmt.Errorf("update activity: %w", err)
		}
		if n == 0 {
			return errNotFound
		}

		current, err := s.GetActivity(ctx, a.ID)
		if err != nil {
			return err
		}
		cur := current.ScheduleOn(now)
		if cur.Kind == sched.Kind && cur.WeekdayMask == sched.WeekdayMask && cur.TimesPerWeek == sched.TimesPerWeek {
			return nil
		}

		before, err := q.CountLogsBefore(ctx, db.CountLogsBeforeParams{ActivityID: a.ID, Day: fmtDay(now)})
		if err != nil {
			return fmt.Errorf("count logs: %w", err)
		}
		if before == 0 {
			if err := q.DeleteSchedulesForActivity(ctx, a.ID); err != nil {
				return fmt.Errorf("delete schedules: %w", err)
			}
			return s.upsertSchedule(ctx, a.ID, sinceForever, sched)
		}
		return s.upsertSchedule(ctx, a.ID, fmtDay(now), sched)
	})
}

func (s *DBStore) SetArchived(ctx context.Context, id int64, archived bool) error {
	var at *string
	if archived {
		ts := time.Now().UTC().Format(time.RFC3339)
		at = &ts
	}
	n, err := s.getQueries(ctx).SetActivityArchived(ctx, db.SetActivityArchivedParams{ArchivedAt: at, ID: id})
	if err != nil {
		return fmt.Errorf("set archived: %w", err)
	}
	if n == 0 {
		return errNotFound
	}
	return nil
}

// MoveActivity swaps the activity with its neighbour (dir -1 = up, +1 =
// down) among activities with the same archived state.
func (s *DBStore) MoveActivity(ctx context.Context, id int64, dir int) error {
	return s.withinTransaction(ctx, func(ctx context.Context) error {
		all, err := s.ListActivities(ctx, true)
		if err != nil {
			return err
		}
		idx := -1
		for i, a := range all {
			if a.ID == id {
				idx = i
			}
		}
		if idx < 0 {
			return errNotFound
		}
		for j := idx + dir; j >= 0 && j < len(all); j += dir {
			if all[j].Archived != all[idx].Archived {
				continue
			}
			q := s.getQueries(ctx)
			// re-number everything so equal sort orders can't get stuck
			all[idx], all[j] = all[j], all[idx]
			for i, a := range all {
				if err := q.SetActivitySortOrder(ctx, db.SetActivitySortOrderParams{SortOrder: int64(i + 1), ID: a.ID}); err != nil {
					return fmt.Errorf("set sort order: %w", err)
				}
			}
			return nil
		}
		return nil
	})
}
