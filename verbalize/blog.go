package blog

import (
	"appengine"
	"appengine/datastore"
	"appengine/user"
	"bytes"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"github.com/kylelemons/go-gypsy/yaml"
	"time"
)

const (
	// Internal constants
	CONFIG_PATH = "verbalize.yml"
	VERSION     = "zero.2012_08_21"
)

type Entry struct {
	Author      string
	IsPublished bool
	PublishDate time.Time
	Title       string
	Content     []byte
	Slug        string
	RelativeUrl string
}

/* return a fetching key for a given entry */
func (e *Entry) Key(c appengine.Context) *datastore.Key {
	return datastore.NewKey(c, "Entries", e.Slug, 0, nil)
}

/* a mirror of Entry, with markings for raw HTML encoding. */
type EntryContext struct {
	Author         string
	IsPublished    bool
	Timestamp      int64
	Day            int
	RfcDate        string
	Hour           int
	Minute         int
	Month          time.Month
	MonthString    string
	Year           int
	Title          string
	Content        template.HTML
	Excerpt        template.HTML
	EscapedExcerpt []byte
	IsExcerpted    bool
	RelativeUrl    string
	Slug           string
}

/* Entry.Context() generates template data from a stored entry */
func (e *Entry) Context() EntryContext {
	log.Printf("More tag is: %v", config.Require("more_tag"))
	content := bytes.Replace(e.Content, []byte(config.Require("more_tag")),
		[]byte("<!-- more tag -->"), 1)
	excerpt := bytes.SplitN(e.Content, []byte(config.Require("more_tag")),
		2)[0]
	return EntryContext{
		Author:         e.Author,
		IsPublished:    e.IsPublished,
		Timestamp:      e.PublishDate.UTC().Unix(),
		Day:            e.PublishDate.Day(),
		Hour:           e.PublishDate.Hour(),
		Minute:         e.PublishDate.Minute(),
		Month:          e.PublishDate.Month(),
		MonthString:    e.PublishDate.Month().String(),
		Year:           e.PublishDate.Year(),
		RfcDate:        e.PublishDate.Format(time.RFC3339),
		Title:          e.Title,
		Content:        template.HTML(content),
		Excerpt:        template.HTML(excerpt),
		EscapedExcerpt: excerpt,
		IsExcerpted:    len(content) != len(excerpt),
		RelativeUrl:    e.RelativeUrl,
		Slug:           e.Slug,
	}
}

/* This is sent to all templates */
type TemplateContext struct {
	Title           string
	SubTitle        string
	BaseUrl         template.HTML
	PageTitle       string
	Description     string
	Version         string
	PageTimeRfc3339 string
	PageTimestamp   int64
	Entries         []EntryContext
	Links           []Link
	DisqusId        string
	Hostname        template.HTML
}

/* Structure used for querying for blog entries */
type EntryQuery struct {
	Start time.Time
	End   time.Time
	Count int
	Tag   string // unused
}

type Link struct {
	Title string
	Url   string
}

var (
	config = yaml.ConfigFile(CONFIG_PATH)

	archive_tmpl = template.Must(template.ParseFiles(
		"templates/base.html",
		"templates/archive.html",
	))
	entry_tmpl = template.Must(template.ParseFiles(
		"templates/base.html",
		"templates/entry.html",
	))
	error_tmpl = template.Must(template.ParseFiles(
		"templates/base.html",
		"templates/error.html",
	))
	edit_tmpl = template.Must(template.ParseFiles(
		"templates/base.html",
		"templates/edit.html",
	))
	admin_tmpl = template.Must(template.ParseFiles(
		"templates/base.html",
		"templates/admin.html",
	))
	feed_tmpl = template.Must(template.ParseFiles(
		"templates/feed.html",
	))
	// regexp matching an entry URL
	view_entry_re = regexp.MustCompile(`\d{4}/\d{2}/(?P<slug>[\w-]+)$`)
	edit_entry_re = regexp.MustCompile(`edit/(?P<slug>[\w-]+)$`)
)

// Setup the URL handlers at initialization
func init() {
	/* ServeMux does not understand regular expressions :( */
	http.HandleFunc("/", root)
	http.HandleFunc("/admin/edit/", edit)
	http.HandleFunc("/admin/", admin)
	http.HandleFunc("/submit", submit)
	http.HandleFunc("/feed/", feed)
}

// Render a named template name to the HTTP channel
func renderTemplate(w http.ResponseWriter, tmpl template.Template, context interface{}) {
	err := tmpl.ExecuteTemplate(w, "base.html", context)
	if err != nil {
		log.Printf("ERROR: %s", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func GetTemplateContext(entries []Entry, title string, r *http.Request) (t TemplateContext, err error) {
	entry_contexts := make([]EntryContext, 0, len(entries))
	for _, entry := range entries {
		entry_contexts = append(entry_contexts, entry.Context())
	}

	/* See https://groups.google.com/forum/?fromgroups=#!topic/golang-nuts/ANpkd4zyjLU */
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	base_url := scheme + "://" + r.Host + config.Require("subdirectory")

	t = TemplateContext{
		BaseUrl:         template.HTML(base_url),
		Title:           config.Require("title"),
		SubTitle:        config.Require("subtitle"),
		Description:     config.Require("description"),
		Version:         VERSION,
		PageTimeRfc3339: time.Now().Format(time.RFC3339),
		PageTimestamp:   time.Now().Unix() * 1000,
		Entries:         entry_contexts,
		PageTitle:       title,
		DisqusId:        config.Require("disqus_id"),
	}
	return t, err
}

func GetEntries(c appengine.Context, params EntryQuery) (entries []Entry, err error) {
	if params.Count == 0 {
		params.Count, _ = strconv.Atoi(config.Require("entries_per_page"))
	}
	q := datastore.NewQuery("Entries").Order("-PublishDate").Limit(params.Count)
	if params.Start.IsZero() {
		q.Filter("PublishDate >", params.End)
	}
	if params.End.IsZero() {
		q.Filter("PublishDate <", params.End)
	}

	entries = make([]Entry, 0, params.Count)
	_, err = q.GetAll(c, &entries)
	return entries, err
}

func GetSingleEntry(c appengine.Context, slug string) (e Entry, err error) {
	e.Slug = slug
	err = datastore.Get(c, e.Key(c), &e)
	return
}

// HTTP handler for rendering blog entries
func root(w http.ResponseWriter, r *http.Request) {
	log.Printf("%s", r.URL.Path)
	template := *error_tmpl
	title := "Error"
	entries := make([]Entry, 0)
	c := appengine.NewContext(r)

	if r.URL.Path == "/" {
		title = "Archive"
		template = *archive_tmpl
		entries, _ = GetEntries(c, EntryQuery{})
	}

	matches := view_entry_re.FindStringSubmatch(r.URL.Path)
	if len(matches) > 0 {
		entry, _ := GetSingleEntry(c, matches[1])
		entries = append(entries, entry)
		template = *entry_tmpl
		title = entries[0].Title
	}

	if len(entries) == 0 {
		http.Error(w, "Nothing to see here.", http.StatusNotFound)
	} else {
		context, _ := GetTemplateContext(entries, title, r)
		renderTemplate(w, template, context)
	}
}

// HTTP handler for /feed
func feed(w http.ResponseWriter, r *http.Request) {
	log.Printf("%v", r.URL)
	c := appengine.NewContext(r)
	entries, _ := GetEntries(c, EntryQuery{})
	context, _ := GetTemplateContext(entries, "feed", r)
	err := feed_tmpl.Execute(w, context)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// HTTP handler for /admin
func admin(w http.ResponseWriter, r *http.Request) {
	c := appengine.NewContext(r)
	entries, _ := GetEntries(c, EntryQuery{})
	context, _ := GetTemplateContext(entries, "Admin", r)
	err := admin_tmpl.Execute(w, context)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// HTTP handler for /edit
func edit(w http.ResponseWriter, r *http.Request) {
	c := appengine.NewContext(r)
	entries := make([]Entry, 0)
	matches := edit_entry_re.FindStringSubmatch(r.URL.Path)
	log.Printf("%s: %v", r.URL.Path, matches)
	title := "New"
	if len(matches) > 0 {
		entry, _ := GetSingleEntry(c, matches[1])
		entries = append(entries, entry)
		title = entries[0].Title
	}
	context, _ := GetTemplateContext(entries, title, r)
	renderTemplate(w, *edit_tmpl, context)
}

// HTTP handler for /submit - submits a blog entry into datastore
func submit(w http.ResponseWriter, r *http.Request) {
	content := strings.TrimSpace(r.FormValue("content"))
	title := strings.TrimSpace(r.FormValue("title"))
	slug := strings.TrimSpace(r.FormValue("slug"))
	entry := Entry{}

	if len(content) == 0 {
		http.Error(w, "No content", http.StatusInternalServerError)
		return
	}
	if len(title) == 0 {
		http.Error(w, "No title", http.StatusInternalServerError)
		return
	}

	c := appengine.NewContext(r)

	if len(slug) == 0 {
		if u := user.Current(c); u != nil {
			entry.Author = u.String()
		}
	} else {
		entry, _ = GetSingleEntry(c, slug)
	}

	entry.Content = []byte(content)
	entry.Title = title
	entry.Slug = slug

	if entry.PublishDate.IsZero() {
		entry.PublishDate = time.Now()
	}

	entry.RelativeUrl = fmt.Sprintf("%d/%02d/%s", entry.PublishDate.Year(),
		entry.PublishDate.Month(), entry.Slug)

	_, err := datastore.Put(c, entry.Key(c), &entry)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	log.Printf("Saved entry: %v", entry)
	http.Redirect(w, r, "/", http.StatusFound)
}
