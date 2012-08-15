package blog

import (
	"appengine"
	"appengine/datastore"
	"appengine/user"
	"fmt"
	"html/template"
	"net/http"
	"strings"
	"time"
)

const (
	VERSION   = "zero.2012_08_12"
	SITE_NAME = "ver&bull;bal&bull;ize"
)

type Entry struct {
	Author      string
	PublishDate time.Time
	Title       string
	Content     []byte
	Slug        string
	RelativeUrl string
}

/* a mirror of Entry, with markings for raw HTML encoding. */
type EntryContext struct {
	Author      string
	Timestamp   int64
	Day         int
	Hour        int
	Minute      int
	Month       time.Month
	MonthString string
	Year        int
	Title       string
	Content     template.HTML
	RelativeUrl template.HTML
}

/* Entry.Context() generates template data from a stored entry */
func (e *Entry) Context() EntryContext {
	return EntryContext{
		Author:      e.Author,
		Timestamp:   e.PublishDate.UTC().Unix(),
		Day:         e.PublishDate.Day(),
		Hour:        e.PublishDate.Hour(),
		Minute:      e.PublishDate.Minute(),
		Month:       e.PublishDate.Month(),
		MonthString: e.PublishDate.Month().String(),
		Year:        e.PublishDate.Year(),
		Title:       e.Title,
		Content:     template.HTML(e.Content),
		RelativeUrl: template.HTML(e.RelativeUrl),
	}
}

type TemplateContext struct {
	SiteName template.HTML
	Version  string
	Title    template.HTML
	Entries  []EntryContext
}

var (
	blog_tmpl = template.Must(template.ParseFiles(
		"templates/base.html",
		"templates/blog.html",
	))
	edit_tmpl = template.Must(template.ParseFiles(
		"templates/base.html",
		"templates/edit.html",
	))
)

// Setup the URL handlers at initialization
func init() {
	http.HandleFunc("/", root)
	http.HandleFunc("/edit", edit)
	http.HandleFunc("/submit", submit)
}

// Render a named template name to the HTTP channel
func renderTemplate(w http.ResponseWriter, tmpl template.Template, context interface{}) {
	err := tmpl.ExecuteTemplate(w, "base.html", context)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// HTTP handler for /
func root(w http.ResponseWriter, r *http.Request) {
	context := appengine.NewContext(r)
	query := datastore.NewQuery("Entries").Order("-PublishDate").Limit(10)
	entry_contexts := make([]EntryContext, 0, 10)

	for cursor := query.Run(context); ; {
		var entry Entry
		_, err := cursor.Next(&entry)
		if err == datastore.Done {
			break
		}
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		entry_contexts = append(entry_contexts, entry.Context())
	}
	template_context := TemplateContext{
		SiteName: SITE_NAME,
		Version:  VERSION,
		Entries:  entry_contexts,
		Title:    "blog entries",
	}
	renderTemplate(w, *blog_tmpl, template_context)
}

// HTTP handler for /edit
func edit(w http.ResponseWriter, r *http.Request) {
	context := TemplateContext{
		SiteName: SITE_NAME,
		Version:  VERSION,
	}
	renderTemplate(w, *edit_tmpl, context)
}

// HTTP handler for /submit - submits a blog entry into datastore
func submit(w http.ResponseWriter, r *http.Request) {
	content := strings.TrimSpace(r.FormValue("content"))
	title := strings.TrimSpace(r.FormValue("title"))
	slug := strings.TrimSpace(r.FormValue("slug"))
	if len(content) == 0 {
		http.Error(w, "No content", http.StatusInternalServerError)
		return
	}
	if len(title) == 0 {
		http.Error(w, "No title", http.StatusInternalServerError)
		return
	}

	publish_date := time.Now()
	/* TODO(tstromberg): Find a faster way */
	relative_url := fmt.Sprintf("%d/%02d/%s", publish_date.Year(),
		publish_date.Month(), slug)
	c := appengine.NewContext(r)
	g := Entry{
		Content:     []byte(content),
		Title:       title,
		Slug:        slug,
		RelativeUrl: relative_url,
		PublishDate: publish_date,
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
