package main

import (
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"
)

var defaultColors = []string{"#4e79a7", "#f28e2b", "#59a14f", "#e15759", "#b07aa1", "#76b7b2", "#edc948", "#9c755f"}

var colorRe = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

type activitiesPage struct {
	Title    string
	Nav      string
	Active   []Activity
	Archived []Activity
}

type activityForm struct {
	Name         string
	Unit         string
	Color        string
	Kind         ScheduleKind
	WeekdayMask  uint8
	TimesPerWeek int
}

type activityFormPage struct {
	Title    string
	Nav      string
	ID       int64 // 0 = new
	Form     activityForm
	Problems map[string]string
}

func parseActivityForm(r *http.Request) (activityForm, map[string]string) {
	f := activityForm{
		Name:  strings.TrimSpace(r.FormValue("name")),
		Unit:  strings.TrimSpace(r.FormValue("unit")),
		Color: r.FormValue("color"),
		Kind:  ScheduleKind(r.FormValue("kind")),
	}
	problems := map[string]string{}

	if f.Name == "" {
		problems["name"] = "Name is required"
	}
	if !colorRe.MatchString(f.Color) {
		f.Color = defaultColors[0]
	}
	for _, d := range r.Form["days"] {
		if i, err := strconv.Atoi(d); err == nil && i >= 0 && i < 7 {
			f.WeekdayMask |= 1 << i
		}
	}
	f.TimesPerWeek, _ = strconv.Atoi(r.FormValue("times"))

	switch f.Kind {
	case ScheduleWeekdays:
		if f.WeekdayMask == 0 {
			problems["schedule"] = "Pick at least one weekday"
		}
	case ScheduleWeekly:
		if f.TimesPerWeek < 1 || f.TimesPerWeek > 7 {
			problems["schedule"] = "Times per week must be between 1 and 7"
		}
	default:
		problems["schedule"] = "Pick a schedule type"
	}
	return f, problems
}

func (f activityForm) schedule() Schedule {
	s := Schedule{Kind: f.Kind}
	if f.Kind == ScheduleWeekly {
		s.TimesPerWeek = f.TimesPerWeek
	} else {
		s.WeekdayMask = f.WeekdayMask
	}
	return s
}

func (f activityForm) activity(id int64) Activity {
	return Activity{ID: id, Name: f.Name, Unit: f.Unit, Color: f.Color}
}

func handleActivitiesPage(store *DBStore, tmpl *Templates) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		all, err := store.ListActivities(r.Context(), true)
		if err != nil {
			getLogger(r.Context()).Error("error listing activities", "err", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}
		page := activitiesPage{Title: "Activities", Nav: "activities"}
		for _, a := range all {
			if a.Archived {
				page.Archived = append(page.Archived, a)
			} else {
				page.Active = append(page.Active, a)
			}
		}
		tmpl.render(w, r, "activities", page)
	}
}

func handleNewActivityPage(store *DBStore, tmpl *Templates) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		all, err := store.ListActivities(r.Context(), true)
		if err != nil {
			getLogger(r.Context()).Error("error listing activities", "err", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}
		tmpl.render(w, r, "activity_form", activityFormPage{
			Title: "New activity",
			Nav:   "activities",
			Form: activityForm{
				Color:        defaultColors[len(all)%len(defaultColors)],
				Kind:         ScheduleWeekdays,
				WeekdayMask:  0x7f,
				TimesPerWeek: 3,
			},
		})
	}
}

func handleCreateActivity(store *DBStore, tmpl *Templates) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		form, problems := parseActivityForm(r)
		if len(problems) > 0 {
			w.WriteHeader(http.StatusUnprocessableEntity)
			tmpl.render(w, r, "activity_form", activityFormPage{Title: "New activity", Nav: "activities", Form: form, Problems: problems})
			return
		}
		if _, err := store.CreateActivity(r.Context(), form.activity(0), form.schedule(), today()); err != nil {
			getLogger(r.Context()).Error("error creating activity", "err", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}
		http.Redirect(w, r, "/activities", http.StatusSeeOther)
	}
}

func handleEditActivityPage(store *DBStore, tmpl *Templates) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := parseIDParam(r)
		if err != nil {
			http.Error(w, "invalid id", http.StatusBadRequest)
			return
		}
		a, err := store.GetActivity(r.Context(), id)
		if errors.Is(err, errNotFound) {
			http.Error(w, "activity not found", http.StatusNotFound)
			return
		} else if err != nil {
			getLogger(r.Context()).Error("error getting activity", "err", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}
		s := a.CurrentSchedule()
		form := activityForm{Name: a.Name, Unit: a.Unit, Color: a.Color, Kind: s.Kind, WeekdayMask: s.WeekdayMask, TimesPerWeek: s.TimesPerWeek}
		if form.TimesPerWeek == 0 {
			form.TimesPerWeek = 3
		}
		if form.WeekdayMask == 0 {
			form.WeekdayMask = 0x7f
		}
		tmpl.render(w, r, "activity_form", activityFormPage{Title: "Edit " + a.Name, Nav: "activities", ID: id, Form: form})
	}
}

func handleUpdateActivity(store *DBStore, tmpl *Templates) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := parseIDParam(r)
		if err != nil {
			http.Error(w, "invalid id", http.StatusBadRequest)
			return
		}
		form, problems := parseActivityForm(r)
		if len(problems) > 0 {
			w.WriteHeader(http.StatusUnprocessableEntity)
			tmpl.render(w, r, "activity_form", activityFormPage{Title: "Edit activity", Nav: "activities", ID: id, Form: form, Problems: problems})
			return
		}
		if err := store.UpdateActivity(r.Context(), form.activity(id), form.schedule(), today()); errors.Is(err, errNotFound) {
			http.Error(w, "activity not found", http.StatusNotFound)
			return
		} else if err != nil {
			getLogger(r.Context()).Error("error updating activity", "err", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}
		http.Redirect(w, r, "/activities", http.StatusSeeOther)
	}
}

func handleArchiveActivity(store *DBStore, archived bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := parseIDParam(r)
		if err != nil {
			http.Error(w, "invalid id", http.StatusBadRequest)
			return
		}
		if err := store.SetArchived(r.Context(), id, archived); errors.Is(err, errNotFound) {
			http.Error(w, "activity not found", http.StatusNotFound)
			return
		} else if err != nil {
			getLogger(r.Context()).Error("error archiving activity", "err", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}
		http.Redirect(w, r, "/activities", http.StatusSeeOther)
	}
}

func handleMoveActivity(store *DBStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := parseIDParam(r)
		if err != nil {
			http.Error(w, "invalid id", http.StatusBadRequest)
			return
		}
		dir := 1
		if r.FormValue("dir") == "up" {
			dir = -1
		}
		if err := store.MoveActivity(r.Context(), id, dir); errors.Is(err, errNotFound) {
			http.Error(w, "activity not found", http.StatusNotFound)
			return
		} else if err != nil {
			getLogger(r.Context()).Error("error moving activity", "err", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}
		http.Redirect(w, r, "/activities", http.StatusSeeOther)
	}
}
