package main

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type dayRow struct {
	Day       string
	Activity  Activity
	Schedule  Schedule
	Entry     *LogEntry
	WeekCount int
}

func (r dayRow) Weekly() bool { return r.Schedule.Kind == ScheduleWeekly }

// Due: weekday schedules due on this weekday; weekly schedules whose target
// isn't met yet by the other days of the week (so toggling today doesn't
// move the row around).
func (r dayRow) Due() bool {
	if r.Weekly() {
		other := r.WeekCount
		if r.Entry != nil {
			other--
		}
		return other < r.Schedule.TimesPerWeek
	}
	return r.Schedule.DueOn(mustParseDay(r.Day))
}

func mustParseDay(s string) time.Time {
	d, _ := parseDay(s)
	return d
}

type dayPage struct {
	Title   string
	Nav     string
	Day     time.Time
	IsToday bool
	Prev    string
	Next    string // "" if Day is today
	Due     []dayRow
	NotDue  []dayRow
	Note    string
}

// parseDayParam parses the {date} path value; "today" and future days
// resolve to today.
func parseDayParam(r *http.Request) (time.Time, error) {
	now := today()
	s := r.PathValue("date")
	if s == "today" || s == "" {
		return now, nil
	}
	d, err := parseDay(s)
	if err != nil {
		return time.Time{}, err
	}
	if d.After(now) {
		return now, nil
	}
	return d, nil
}

func parseIDParam(r *http.Request) (int64, error) {
	return strconv.ParseInt(r.PathValue("id"), 10, 64)
}

func isHtmx(r *http.Request) bool {
	return r.Header.Get("HX-Request") != ""
}

func handleRoot() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/day/"+fmtDay(today()), http.StatusSeeOther)
	}
}

func handleDayPage(store *DBStore, tmpl *Templates) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		logger := getLogger(r.Context())

		day, err := parseDayParam(r)
		if err != nil {
			http.Error(w, "invalid date", http.StatusBadRequest)
			return
		}
		if r.PathValue("date") != fmtDay(day) {
			http.Redirect(w, r, "/day/"+fmtDay(day), http.StatusSeeOther)
			return
		}

		activities, err := store.ListActivities(r.Context(), false)
		if err != nil {
			logger.Error("error listing activities", "err", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}
		monday := mondayOf(day)
		logs, err := store.LogsInRange(r.Context(), monday, monday.AddDate(0, 0, 6))
		if err != nil {
			logger.Error("error listing logs", "err", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}
		notes, err := store.DayNotesInRange(r.Context(), day, day)
		if err != nil {
			logger.Error("error listing day notes", "err", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}

		now := today()
		page := dayPage{
			Title:   day.Format("Mon 2 Jan"),
			Nav:     "day",
			Day:     day,
			IsToday: day.Equal(now),
			Prev:    fmtDay(day.AddDate(0, 0, -1)),
			Note:    notes[day],
		}
		if !page.IsToday {
			page.Next = fmtDay(day.AddDate(0, 0, 1))
		}
		for _, a := range activities {
			row := buildDayRow(a, logs[a.ID], day)
			if row.Due() {
				page.Due = append(page.Due, row)
			} else {
				page.NotDue = append(page.NotDue, row)
			}
		}

		tmpl.render(w, r, "day", page)
	}
}

func buildDayRow(a Activity, logs ActivityLogs, day time.Time) dayRow {
	row := dayRow{
		Day:       fmtDay(day),
		Activity:  a,
		Schedule:  a.ScheduleOn(day),
		WeekCount: logs.CountInWeek(day),
	}
	if e, ok := logs[day]; ok {
		row.Entry = &e
	}
	return row
}

// respondDayRow re-renders the activity's row for htmx, or redirects back
// to the day page for plain form posts.
func respondDayRow(w http.ResponseWriter, r *http.Request, store *DBStore, tmpl *Templates, id int64, day time.Time) {
	if !isHtmx(r) {
		http.Redirect(w, r, "/day/"+fmtDay(day), http.StatusSeeOther)
		return
	}
	logger := getLogger(r.Context())

	a, err := store.GetActivity(r.Context(), id)
	if err != nil {
		logger.Error("error getting activity", "err", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	monday := mondayOf(day)
	logs, err := store.LogsInRange(r.Context(), monday, monday.AddDate(0, 0, 6))
	if err != nil {
		logger.Error("error listing logs", "err", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	tmpl.renderFragment(w, r, "day", "day_row", buildDayRow(a, logs[id], day))
}

func handleToggleLog(store *DBStore, tmpl *Templates) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		day, err := parseDayParam(r)
		if err != nil {
			http.Error(w, "invalid date", http.StatusBadRequest)
			return
		}
		id, err := parseIDParam(r)
		if err != nil {
			http.Error(w, "invalid id", http.StatusBadRequest)
			return
		}
		if _, err := store.GetActivity(r.Context(), id); errors.Is(err, errNotFound) {
			http.Error(w, "activity not found", http.StatusNotFound)
			return
		}

		if err := store.ToggleLog(r.Context(), id, day); err != nil {
			getLogger(r.Context()).Error("error toggling log", "err", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}
		respondDayRow(w, r, store, tmpl, id, day)
	}
}

// handleSaveLog sets amount and note, marking the activity as done.
func handleSaveLog(store *DBStore, tmpl *Templates) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		day, err := parseDayParam(r)
		if err != nil {
			http.Error(w, "invalid date", http.StatusBadRequest)
			return
		}
		id, err := parseIDParam(r)
		if err != nil {
			http.Error(w, "invalid id", http.StatusBadRequest)
			return
		}
		if _, err := store.GetActivity(r.Context(), id); errors.Is(err, errNotFound) {
			http.Error(w, "activity not found", http.StatusNotFound)
			return
		}

		var amount *float64
		if s := strings.TrimSpace(strings.ReplaceAll(r.FormValue("amount"), ",", ".")); s != "" {
			v, err := strconv.ParseFloat(s, 64)
			if err != nil {
				http.Error(w, "invalid amount", http.StatusBadRequest)
				return
			}
			amount = &v
		}

		if err := store.SaveLog(r.Context(), id, day, amount, strings.TrimSpace(r.FormValue("note"))); err != nil {
			getLogger(r.Context()).Error("error saving log", "err", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}
		respondDayRow(w, r, store, tmpl, id, day)
	}
}

func handleSaveDayNote(store *DBStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		day, err := parseDayParam(r)
		if err != nil {
			http.Error(w, "invalid date", http.StatusBadRequest)
			return
		}
		if err := store.SetDayNote(r.Context(), day, strings.TrimSpace(r.FormValue("note"))); err != nil {
			getLogger(r.Context()).Error("error saving day note", "err", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}
		http.Redirect(w, r, "/day/"+fmtDay(day), http.StatusSeeOther)
	}
}
