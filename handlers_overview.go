package main

import (
	"encoding/csv"
	"net/http"
	"slices"
	"strings"
	"time"
)

// --- calendar ---

type calDay struct {
	Day     string
	Num     int
	Weekday string
	Today   bool
	Future  bool
	Weekend bool
	Note    string
}

type calCell struct {
	Day    string
	Entry  *LogEntry
	Due    bool
	Missed bool
	Future bool
}

type calRow struct {
	Activity Activity
	Label    string
	Cells    []calCell
	Count    int
}

type calendarPage struct {
	Title string
	Nav   string
	Month string // "November 2026"
	Prev  string // YYYY-MM
	Next  string // "" if current month
	Days  []calDay
	Rows  []calRow
}

func handleCalendarPage(store *DBStore, tmpl *Templates) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		logger := getLogger(r.Context())
		now := today()

		first := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
		if m := r.URL.Query().Get("month"); m != "" {
			t, err := time.Parse("2006-01", m)
			if err != nil {
				http.Error(w, "invalid month", http.StatusBadRequest)
				return
			}
			if !t.After(first) {
				first = t
			}
		}
		last := first.AddDate(0, 1, -1)

		activities, err := store.ListActivities(r.Context(), true)
		if err != nil {
			logger.Error("error listing activities", "err", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}
		logs, err := store.LogsInRange(r.Context(), first, last)
		if err != nil {
			logger.Error("error listing logs", "err", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}
		notes, err := store.DayNotesInRange(r.Context(), first, last)
		if err != nil {
			logger.Error("error listing day notes", "err", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}

		page := calendarPage{
			Title: first.Format("January 2006"),
			Nav:   "calendar",
			Month: first.Format("January 2006"),
			Prev:  first.AddDate(0, -1, 0).Format("2006-01"),
		}
		if first.Before(time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)) {
			page.Next = first.AddDate(0, 1, 0).Format("2006-01")
		}
		for d := first; !d.After(last); d = d.AddDate(0, 0, 1) {
			wd := weekdayIndex(d)
			page.Days = append(page.Days, calDay{
				Day: fmtDay(d), Num: d.Day(), Weekday: weekdayShort[wd],
				Today: d.Equal(now), Future: d.After(now), Weekend: wd >= 5, Note: notes[d],
			})
		}

		for _, a := range activities {
			aLogs := logs[a.ID]
			if a.Archived && len(aLogs) == 0 {
				continue
			}
			start := trackingStart(a, aLogs)
			row := calRow{Activity: a, Label: a.ScheduleOn(minDay(last, now)).Label(), Count: len(aLogs)}
			for d := first; !d.After(last); d = d.AddDate(0, 0, 1) {
				c := calCell{Day: fmtDay(d), Future: d.After(now), Due: a.ScheduleOn(d).DueOn(d)}
				if e, ok := aLogs[d]; ok {
					c.Entry = &e
				}
				c.Missed = c.Due && c.Entry == nil && d.Before(now) && !d.Before(start) && !a.Archived
				row.Cells = append(row.Cells, c)
			}
			page.Rows = append(page.Rows, row)
		}

		tmpl.render(w, r, "calendar", page)
	}
}

// --- stats ---

type statsRow struct {
	Activity Activity
	Label    string
	Stats    ActivityStats
}

type statsPage struct {
	Title string
	Nav   string
	Range string
	From  string
	To    string
	Rows  []statsRow
}

func handleStatsPage(store *DBStore, tmpl *Templates) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		logger := getLogger(r.Context())
		now := today()

		activities, err := store.ListActivities(r.Context(), true)
		if err != nil {
			logger.Error("error listing activities", "err", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}
		logs, err := store.AllLogs(r.Context())
		if err != nil {
			logger.Error("error listing logs", "err", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}

		rng := r.URL.Query().Get("range")
		from, to := now, now
		switch rng {
		case "week":
			from = mondayOf(now)
		case "year":
			from = time.Date(now.Year(), 1, 1, 0, 0, 0, 0, time.UTC)
		case "all":
			for _, a := range activities {
				from = minDay(from, trackingStart(a, logs[a.ID]))
			}
		case "custom":
			f, err1 := parseDay(r.URL.Query().Get("from"))
			t, err2 := parseDay(r.URL.Query().Get("to"))
			if err1 != nil || err2 != nil || t.Before(f) {
				http.Error(w, "invalid from/to", http.StatusBadRequest)
				return
			}
			from, to = f, minDay(t, now)
		default:
			rng = "month"
			from = time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
		}

		page := statsPage{Title: "Stats", Nav: "stats", Range: rng, From: fmtDay(from), To: fmtDay(to)}
		for _, a := range activities {
			st := ComputeStats(a, logs[a.ID], from, to, now)
			if a.Archived && st.DoneCount == 0 {
				continue
			}
			page.Rows = append(page.Rows, statsRow{Activity: a, Label: a.ScheduleOn(minDay(to, now)).Label(), Stats: st})
		}

		tmpl.render(w, r, "stats", page)
	}
}

// --- export ---

func handleExportCSV(store *DBStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		logger := getLogger(r.Context())

		activities, err := store.ListActivities(r.Context(), true)
		if err != nil {
			logger.Error("error listing activities", "err", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}
		logs, err := store.AllLogs(r.Context())
		if err != nil {
			logger.Error("error listing logs", "err", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}
		notes, err := store.DayNotesInRange(r.Context(), time.Time{}, time.Date(9999, 1, 1, 0, 0, 0, 0, time.UTC))
		if err != nil {
			logger.Error("error listing day notes", "err", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}

		records := [][]string{}
		for _, a := range activities {
			for d, e := range logs[a.ID] {
				records = append(records, []string{fmtDay(d), a.Name, fmtAmount(e.Amount), a.Unit, e.Note})
			}
		}
		for d, n := range notes {
			records = append(records, []string{fmtDay(d), "(journal)", "", "", n})
		}
		slices.SortFunc(records, func(a, b []string) int {
			if c := strings.Compare(a[0], b[0]); c != 0 {
				return c
			}
			return strings.Compare(a[1], b[1])
		})

		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="tracker-`+fmtDay(today())+`.csv"`)
		cw := csv.NewWriter(w)
		_ = cw.Write([]string{"date", "activity", "amount", "unit", "note"})
		_ = cw.WriteAll(records)
	}
}

