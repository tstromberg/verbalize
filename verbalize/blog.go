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
	"strings"
	"time"
)

const (
	// TODO(tstromberg): Move user config into YAML
	SITE_NAME        = "foci"
	DISQUS_ID        = "tstromberg-blog"
	BASE_URL         = "http://localhost:8080"
	AUTHOR_NAME      = "unknown"
	AUTHOR_EMAIL     = "nobody@[127.0.0.1]"
	SITE_TITLE       = "verbalize"
	SITE_SUBTITLE    = "AppEngine+Go Blogging Engine"
	ENTRIES_PER_PAGE = 5

	// Internal constants
	// Note: <!--more--> is what wordpress uses. wymeditor removes comments,
	// making this somewhat annoying to use.
	MORE_TAG        = "[[more]]"
	VERSION         = "zero.2012_08_16"
	MAX_ENTRY_SLOTS = 100 // No page should ever request more than this
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
	EscapedExcerpt string
	Url            string
	Slug           string
}

/* Entry.Context() generates template data from a stored entry */
func (e *Entry) Context() EntryContext {
	content := bytes.Replace(e.Content, []byte(MORE_TAG), []byte("<!--more-->"),
		1)
	log.Printf("CON: %s", content)
	excerpt := strings.SplitN(string(e.Content), MORE_TAG, 2)[0]
	log.Printf("EXC: %s", excerpt)

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
		Url:            BASE_URL + "/" + e.RelativeUrl,
		Slug:           e.Slug,
	}
}

/* This is sent to all templates */
type TemplateContext struct {
	SiteName     template.HTML
	SiteUrl      string
	SiteTitle    string
	SiteSubTitle string
	Version      string
	UpdateTime   string
	Title        string
	Entries      []EntryContext
	DisqusId     string
}

/* Structure used for querying for blog entries */
type EntryQuery struct {
	Start time.Time
	End   time.Time
	Count int
	Tag   string // unused
}

var (
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
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func GetTemplateContext(entries []Entry, title string) (t TemplateContext, err error) {
	entry_contexts := make([]EntryContext, 0, len(entries))
	for _, entry := range entries {
		entry_contexts = append(entry_contexts, entry.Context())
	}
	t = TemplateContext{
		SiteName:     SITE_NAME,
		SiteUrl:      BASE_URL,
		SiteTitle:    SITE_TITLE,
		SiteSubTitle: SITE_SUBTITLE,
		Version:      VERSION,
		UpdateTime:   time.Now().Format(time.RFC3339),
		Entries:      entry_contexts,
		Title:        title,
		DisqusId:     DISQUS_ID,
	}
	return t, err
}

func GetEntries(c appengine.Context, params EntryQuery) (entries []Entry, err error) {
	if params.Count == 0 {
		params.Count = ENTRIES_PER_PAGE
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
		context, _ := GetTemplateContext(entries, title)
		renderTemplate(w, template, context)
	}
}

// HTTP handler for /feed
func feed(w http.ResponseWriter, r *http.Request) {
	c := appengine.NewContext(r)
	entries, _ := GetEntries(c, EntryQuery{})
	context, _ := GetTemplateContext(entries, "feed")
	err := feed_tmpl.Execute(w, context)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// HTTP handler for /admin
func admin(w http.ResponseWriter, r *http.Request) {
	c := appengine.NewContext(r)
	entries, _ := GetEntries(c, EntryQuery{})
	context, _ := GetTemplateContext(entries, "Admin")
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
	context, _ := GetTemplateContext(entries, title)
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
