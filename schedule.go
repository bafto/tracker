package main

import (
	"fmt"
	"math/bits"
	"strings"
	"time"
)

// Days are represented as time.Time at 00:00 UTC, so that they can be
// compared, used as map keys and stepped with AddDate without DST issues.
// "Today" is determined in the server's local timezone (TZ env).

const dayLayout = "2006-01-02"

// sinceForever is the effective_from of an activity's initial schedule, so
// that backfilled days before the activity was created are covered too.
const sinceForever = "1970-01-01"

func parseDay(s string) (time.Time, error) {
	return time.Parse(dayLayout, s)
}

func fmtDay(d time.Time) string {
	return d.Format(dayLayout)
}

func toDay(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

// today returns the current day in the server's local timezone.
func today() time.Time {
	return toDay(time.Now().In(time.Local))
}

// weekdayIndex returns 0 for Monday ... 6 for Sunday.
func weekdayIndex(d time.Time) int {
	return (int(d.Weekday()) + 6) % 7
}

// mondayOf returns the Monday of the (ISO, Monday-start) week containing d.
func mondayOf(d time.Time) time.Time {
	return d.AddDate(0, 0, -weekdayIndex(d))
}

type ScheduleKind string

const (
	ScheduleWeekdays ScheduleKind = "weekdays"
	ScheduleWeekly   ScheduleKind = "weekly"
)

var weekdayShort = [7]string{"Mo", "Tu", "We", "Th", "Fr", "Sa", "Su"}

type Schedule struct {
	EffectiveFrom time.Time
	Kind          ScheduleKind
	WeekdayMask   uint8 // bit0 = Monday ... bit6 = Sunday
	TimesPerWeek  int
}

// DueOn reports whether a weekdays-schedule is due on d. Weekly schedules
// are never due on a specific day.
func (s Schedule) DueOn(d time.Time) bool {
	return s.Kind == ScheduleWeekdays && s.WeekdayMask&(1<<weekdayIndex(d)) != 0
}

// WeeklyTarget is the number of done days per week that meets the schedule.
func (s Schedule) WeeklyTarget() int {
	if s.Kind == ScheduleWeekly {
		return s.TimesPerWeek
	}
	return bits.OnesCount8(s.WeekdayMask)
}

func (s Schedule) Label() string {
	switch s.Kind {
	case ScheduleWeekly:
		if s.TimesPerWeek == 1 {
			return "once a week"
		}
		return fmt.Sprintf("%d× per week", s.TimesPerWeek)
	default:
		if s.WeekdayMask&0x7f == 0x7f {
			return "daily"
		}
		if s.WeekdayMask == 0x1f {
			return "weekdays"
		}
		var days []string
		for i := range 7 {
			if s.WeekdayMask&(1<<i) != 0 {
				days = append(days, weekdayShort[i])
			}
		}
		return strings.Join(days, " ")
	}
}

type Activity struct {
	ID        int64
	Name      string
	Unit      string // "" = no amount tracked
	Color     string
	SortOrder int64
	Archived  bool
	CreatedAt time.Time
	// sorted ascending by EffectiveFrom, never empty for a valid activity
	Schedules []Schedule
}

// ScheduleOn returns the schedule in effect on day d.
func (a Activity) ScheduleOn(d time.Time) Schedule {
	if len(a.Schedules) == 0 {
		return Schedule{Kind: ScheduleWeekdays}
	}
	s := a.Schedules[0]
	for _, cand := range a.Schedules[1:] {
		if cand.EffectiveFrom.After(d) {
			break
		}
		s = cand
	}
	return s
}

func (a Activity) CurrentSchedule() Schedule {
	return a.ScheduleOn(today())
}

type LogEntry struct {
	ActivityID int64
	Day        time.Time
	Amount     *float64
	Note       string
}

// ActivityLogs maps day -> entry for a single activity.
type ActivityLogs map[time.Time]LogEntry

// CountInWeek returns how many days of the week containing d are logged.
func (l ActivityLogs) CountInWeek(d time.Time) int {
	monday := mondayOf(d)
	n := 0
	for i := range 7 {
		if _, ok := l[monday.AddDate(0, 0, i)]; ok {
			n++
		}
	}
	return n
}

func (l ActivityLogs) earliest() (time.Time, bool) {
	var first time.Time
	found := false
	for d := range l {
		if !found || d.Before(first) {
			first, found = d, true
		}
	}
	return first, found
}

// trackingStart is the first day for which missed days count against an
// activity: its creation day, or the earliest backfilled log if earlier.
func trackingStart(a Activity, logs ActivityLogs) time.Time {
	start := a.CreatedAt
	if first, ok := logs.earliest(); ok && first.Before(start) {
		start = first
	}
	return start
}

type ActivityStats struct {
	DoneCount int
	AmountSum float64
	// completion = Achieved / Target; Target == 0 means "nothing was due"
	Achieved      int
	Target        int
	CurrentStreak int
	LongestStreak int
	// "days" or "weeks", depending on the current schedule kind
	StreakUnit string
}

// CompletionPercent returns the completion rate in percent, or -1 if
// nothing was due in the range.
func (s ActivityStats) CompletionPercent() int {
	if s.Target == 0 {
		return -1
	}
	return s.Achieved * 100 / s.Target
}

// ComputeStats evaluates an activity over the inclusive range [from, to],
// given all of its logs (needed for streaks) and the current day.
//
// Completion rules:
//   - weekdays schedules: every due day in the range up to yesterday is one
//     target unit, achieved if logged. Today counts only once it is logged.
//     Logging on non-due days does not inflate the rate.
//   - weekly schedules (schedule in effect on the week's Monday): every week
//     overlapping the range that is complete (Sunday < today) contributes
//     N target units, achieved = min(logged days in that week, N). The
//     running week only contributes once its target is met.
//   - nothing before the tracking start counts as missed; weeks starting
//     before it only count once met.
func ComputeStats(a Activity, logs ActivityLogs, from, to, now time.Time) ActivityStats {
	var st ActivityStats

	for d, e := range logs {
		if d.Before(from) || d.After(to) {
			continue
		}
		st.DoneCount++
		if e.Amount != nil {
			st.AmountSum += *e.Amount
		}
	}

	start := trackingStart(a, logs)
	evalFrom := maxDay(from, start)
	evalTo := minDay(to, now)

	// weekday-kind days
	for d := evalFrom; !d.After(evalTo); d = d.AddDate(0, 0, 1) {
		s := a.ScheduleOn(d)
		if !s.DueOn(d) {
			continue
		}
		_, done := logs[d]
		if d.Equal(now) && !done {
			continue
		}
		st.Target++
		if done {
			st.Achieved++
		}
	}

	// weekly-kind weeks
	if !evalFrom.After(evalTo) {
		for monday := mondayOf(evalFrom); !monday.After(evalTo); monday = monday.AddDate(0, 0, 7) {
			s := a.ScheduleOn(monday)
			if s.Kind != ScheduleWeekly || s.TimesPerWeek <= 0 {
				continue
			}
			count := min(logs.CountInWeek(monday), s.TimesPerWeek)
			met := count >= s.TimesPerWeek
			complete := monday.AddDate(0, 0, 6).Before(now)
			if !met && (!complete || monday.Before(start)) {
				continue
			}
			st.Target += s.TimesPerWeek
			st.Achieved += count
		}
	}

	if a.ScheduleOn(now).Kind == ScheduleWeekly {
		st.StreakUnit = "weeks"
		st.CurrentStreak, st.LongestStreak = weekStreaks(a, logs, start, now)
	} else {
		st.StreakUnit = "days"
		st.CurrentStreak, st.LongestStreak = dayStreaks(a, logs, start, now)
	}

	return st
}

// dayStreaks counts consecutive due days that were logged. Non-due days are
// neutral, an unlogged today does not break the streak, and days under a
// weekly schedule are neutral.
func dayStreaks(a Activity, logs ActivityLogs, start, now time.Time) (current, longest int) {
	for d := start; !d.After(now); d = d.AddDate(0, 0, 1) {
		if !a.ScheduleOn(d).DueOn(d) {
			continue
		}
		if _, done := logs[d]; done {
			current++
			longest = max(longest, current)
		} else if !d.Equal(now) {
			current = 0
		}
	}
	return current, longest
}

// weekStreaks counts consecutive weeks meeting their weekly target (for
// weekdays schedules: all due days of the week logged). The running week
// and the first (partial) week only count once met, never break a streak.
func weekStreaks(a Activity, logs ActivityLogs, start, now time.Time) (current, longest int) {
	thisWeek := mondayOf(now)
	for monday := mondayOf(start); !monday.After(thisWeek); monday = monday.AddDate(0, 0, 7) {
		target := a.ScheduleOn(monday).WeeklyTarget()
		if target == 0 {
			continue
		}
		var met bool
		if s := a.ScheduleOn(monday); s.Kind == ScheduleWeekly {
			met = logs.CountInWeek(monday) >= target
		} else {
			met = true
			for i := range 7 {
				d := monday.AddDate(0, 0, i)
				if _, done := logs[d]; a.ScheduleOn(d).DueOn(d) && !done {
					met = false
					break
				}
			}
		}
		switch {
		case met:
			current++
			longest = max(longest, current)
		case monday.Equal(thisWeek), monday.Before(start):
			// running or partial first week: neutral
		default:
			current = 0
		}
	}
	return current, longest
}

func maxDay(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

func minDay(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}
