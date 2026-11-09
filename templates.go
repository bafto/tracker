package main

import (
	"bytes"
	"embed"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"
)

//go:embed templates/*.gohtml
var templatesFS embed.FS

//go:embed static
var staticFS embed.FS

// Templates holds one template set per page, each consisting of the shared
// layout plus the page's own file (which may define fragments too).
type Templates struct {
	pages map[string]*template.Template
}

func fmtAmount(a *float64) string {
	if a == nil {
		return ""
	}
	return strconv.FormatFloat(*a, 'f', -1, 64)
}

var templateFuncs = template.FuncMap{
	"fmtAmount": fmtAmount,
	"fmtDay": fmtDay,
	"longDay": func(d time.Time) string {
		return d.Format("Monday, 2 January 2006")
	},
	"weekdayShort": func(i int) string { return weekdayShort[i] },
	"hasBit": func(mask uint8, i int) bool { return mask&(1<<i) != 0 },
	"seq":    func(n int) []int { s := make([]int, n); for i := range s { s[i] = i }; return s },
	"truncate": func(s string, n int) string {
		r := []rune(s)
		if len(r) <= n {
			return s
		}
		return string(r[:n]) + "…"
	},
}

func loadTemplates() (*Templates, error) {
	base, err := template.New("").Funcs(templateFuncs).ParseFS(templatesFS, "templates/layout.gohtml")
	if err != nil {
		return nil, fmt.Errorf("parse layout: %w", err)
	}

	files, err := fs.Glob(templatesFS, "templates/*.gohtml")
	if err != nil {
		return nil, err
	}

	t := &Templates{pages: map[string]*template.Template{}}
	for _, f := range files {
		name := strings.TrimSuffix(path.Base(f), ".gohtml")
		if name == "layout" {
			continue
		}
		page, err := template.Must(base.Clone()).ParseFS(templatesFS, f)
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", f, err)
		}
		t.pages[name] = page
	}
	return t, nil
}

// render executes the full page (layout + content).
func (t *Templates) render(w http.ResponseWriter, r *http.Request, page string, data any) {
	t.renderFragment(w, r, page, "layout", data)
}

// renderFragment executes a single named template from a page's set,
// e.g. the day page's "day_row" for htmx swaps.
func (t *Templates) renderFragment(w http.ResponseWriter, r *http.Request, page, name string, data any) {
	tmpl, ok := t.pages[page]
	if !ok {
		getLogger(r.Context()).Error("unknown template page", "page", page)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, name, data); err != nil {
		getLogger(r.Context()).Error("error executing template", "page", page, "name", name, "err", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = buf.WriteTo(w)
}
