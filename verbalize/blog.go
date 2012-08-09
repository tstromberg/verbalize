package blog

import (
	"appengine"
	"appengine/datastore"
	"appengine/user"
	"html/template"
	"net/http"
	"strings"
	"time"
)

type Entry struct {
	Author      string
	PublishDate time.Time
	Title       string
	Intro       string
	Content     string
	RelativeUrl string
}

type TemplateContext struct {
	SiteName string
	Version  string
	Entries  []Entry
}

var (
	templates = template.Must(template.ParseFiles(
		"header.html",
		"blog.html",
		"edit.html",
		"footer.html",
	))
)

// Setup the URL handlers at initialization
func init() {
	http.HandleFunc("/", root)
	http.HandleFunc("/edit", edit)
	http.HandleFunc("/submit", submit)
}

// Render a named template name to the HTTP channel
func renderTemplate(w http.ResponseWriter, name string, context interface{}) {
	err := templates.ExecuteTemplate(w, name, context)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// Render a full page using multiple templates
func renderPage(w http.ResponseWriter, name string, context interface{}) {
	renderTemplate(w, "header.html", context)
	renderTemplate(w, name, context)
	renderTemplate(w, "footer.html", context)
}

// HTTP handler for /
func root(w http.ResponseWriter, r *http.Request) {
	c := appengine.NewContext(r)
	q := datastore.NewQuery("Entries").Order("-PublishDate").Limit(10)
	entries := make([]Entry, 0, 10)
	if _, err := q.GetAll(c, &entries); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	context := TemplateContext{
		SiteName: "verbalize",
		Version:  "0.01",
		Entries:  entries,
	}
	renderPage(w, "blog.html", context)
}

// HTTP handler for /edit
func edit(w http.ResponseWriter, r *http.Request) {
	context := TemplateContext{
		SiteName: "verbalize",
		Version:  "0.01",
	}
	renderPage(w, "edit.html", context)
}

// HTTP handler for /submit - submits a blog entry into datastore
func submit(w http.ResponseWriter, r *http.Request) {
	content := strings.TrimSpace(r.FormValue("content"))
	title := strings.TrimSpace(r.FormValue("title"))
	if len(content) == 0 {
		http.Error(w, "No content", http.StatusInternalServerError)
		return
	}
	if len(title) == 0 {
		http.Error(w, "No title", http.StatusInternalServerError)
		return
	}

	c := appengine.NewContext(r)
	g := Entry{
		Content:     content,
		Title:       title,
		PublishDate: time.Now(),
	}
	if u := user.Current(c); u != nil {
		g.Author = u.String()
	}
	_, err := datastore.Put(c, datastore.NewIncompleteKey(c, "Entries", nil), &g)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/", http.StatusFound)
}
