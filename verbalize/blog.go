package blog

import (
	"appengine"
	"appengine/datastore"
	"appengine/user"
	"html/template"
	"log"
	"net/http"
	"strings"
	"time"
)

const (
	VERSION   = "zero.2012_08_12"
	SITE_NAME = "ver&bull;bal&bull;ize"
)

type StoredEntry struct {
	Author      string
	PublishDate time.Time
	Title       string
	Content     []byte
	RelativeUrl string
}

/* a mirror of StoredEntry, with markings for raw HTML encoding. */
type TemplateEntry struct {
	Author      string
	PublishDate time.Time
	Title       string
	Content     template.HTML
	RelativeUrl string
}

type TemplateContext struct {
	SiteName template.HTML
	Version  string
	Title    template.HTML
	Entries  []TemplateEntry
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
	template_entries := make([]TemplateEntry, 10)

	/* TODO(tstromberg): Do I really need to do this? */
	counter := 0

	for cursor := query.Run(context); ; {
		var stored_entry StoredEntry
		_, err := cursor.Next(&stored_entry)
		if err == datastore.Done {
			break
		}
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		log.Println(counter)
		log.Println(stored_entry.Title)
		counter++
		template_entries[counter] = TemplateEntry{
			Author:      stored_entry.Author,
			PublishDate: stored_entry.PublishDate,
			Title:       stored_entry.Title,
			Content:     template.HTML(stored_entry.Content),
			RelativeUrl: stored_entry.RelativeUrl,
		}
	}
	template_context := TemplateContext{
		SiteName: SITE_NAME,
		Version:  VERSION,
		Entries:  template_entries,
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
	if len(content) == 0 {
		http.Error(w, "No content", http.StatusInternalServerError)
		return
	}
	if len(title) == 0 {
		http.Error(w, "No title", http.StatusInternalServerError)
		return
	}

	c := appengine.NewContext(r)
	g := StoredEntry{
		Content:     []byte(content),
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
