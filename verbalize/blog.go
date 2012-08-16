package blog

import (
	"appengine"
	"appengine/datastore"
	"appengine/user"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"regexp"
	"strings"
	"time"
)

const (
	VERSION = "zero.2012_08_12"

	SITE_NAME     = "ver&bull;bal&bull;ize"
	DISQUS_ID     = ""
	MORE_TAG      = "<!--more-->"
	BASE_URL      = "http://localhost:8080"
	AUTHOR_NAME   = "unknown"
	AUTHOR_EMAIL  = "nobody@[127.0.0.1]"
	SITE_TITLE    = "verbalize"
	SITE_SUBTITLE = "AppEngine+Go Blogging Engine"
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
	Author         string
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
	excerpt := strings.SplitN(string(e.Content), MORE_TAG, 2)[0]
	return EntryContext{
		Author:         e.Author,
		Timestamp:      e.PublishDate.UTC().Unix(),
		Day:            e.PublishDate.Day(),
		Hour:           e.PublishDate.Hour(),
		Minute:         e.PublishDate.Minute(),
		Month:          e.PublishDate.Month(),
		MonthString:    e.PublishDate.Month().String(),
		Year:           e.PublishDate.Year(),
		RfcDate:        e.PublishDate.Format(time.RFC3339),
		Title:          e.Title,
		Content:        template.HTML(e.Content),
		Excerpt:        template.HTML(excerpt),
		EscapedExcerpt: excerpt,
		Url:            BASE_URL + "/" + e.RelativeUrl,
		Slug:           e.Slug,
	}
}

type TemplateContext struct {
	SiteName     template.HTML
	SiteUrl      string
	SiteTitle    string
	SiteSubTitle string
	Version      string
	UpdateTime   string
	Title        template.HTML
	Entries      []EntryContext
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
	edit_tmpl = template.Must(template.ParseFiles(
		"templates/base.html",
		"templates/edit.html",
	))
	feed_tmpl = template.Must(template.ParseFiles(
		"templates/feed.html",
	))
	// regexp matching an entry URL
	entry_url_re, _ = regexp.Compile(`\d{4}/\d{2}/\w+$`)
)

// Setup the URL handlers at initialization
func init() {
	/* ServeMux does not understand regular expressions :( */
	http.HandleFunc("/", root)
	http.HandleFunc("/edit", edit)
	http.HandleFunc("/submit", submit)
	http.HandleFunc("/feed", feed)
}

// Render a named template name to the HTTP channel
func renderTemplate(w http.ResponseWriter, tmpl template.Template, context interface{}) {
	err := tmpl.ExecuteTemplate(w, "base.html", context)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// Get TemplateEntries matching something.
func GetTemplateEntries(r *http.Request, slug string, count int) (t TemplateContext, err error) {
	context := appengine.NewContext(r)
	// scope in Go is odd.
	query := datastore.NewQuery("Entries")
	if slug != "" {
		query = datastore.NewQuery("Entries").Filter("Slug =", slug)
	} else {
		query = datastore.NewQuery("Entries").Order("-PublishDate").Limit(count)
	}
	entry_contexts := make([]EntryContext, 0, count)

	log.Printf("Query: %v", query)
	for cursor := query.Run(context); ; {
		var entry Entry
		_, err := cursor.Next(&entry)
		if err == datastore.Done {
			break
		}
		if err != nil {
			return t, err
		}
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
		Title:        "blog entries",
	}
	return t, err
}

// HTTP handler for /
func root(w http.ResponseWriter, r *http.Request) {
	log.Printf("%s", r.URL.Path)
	rendered := false
	url_parts := strings.Split(r.URL.Path, "/")

	if r.URL.Path == "/" {
		context, err := GetTemplateEntries(r, "", 10)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		renderTemplate(w, *archive_tmpl, context)
		rendered = true
	} else if entry_url_re.MatchString(r.URL.Path) {
		// TODO(tstromberg): Find a way to extract slug from regexp
		slug := url_parts[len(url_parts)-1]
		log.Printf("Looking up slug %s", slug)
		context, err := GetTemplateEntries(r, slug, 1)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		renderTemplate(w, *archive_tmpl, context)
		rendered = true
	}

	if rendered == false {
		http.Error(w, "Go away", http.StatusNotFound)
		return
	}
}

// HTTP handler for /feed
func feed(w http.ResponseWriter, r *http.Request) {
	context, err := GetTemplateEntries(r, "", 10)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	err = feed_tmpl.Execute(w, context)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
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
	relative_url := fmt.Sprintf("%d/%02d/%s", publish_date.Year(),
		publish_date.Month(), slug)
	c := appengine.NewContext(r)
	entry := Entry{
		Content:     []byte(content),
		Title:       title,
		Slug:        slug,
		RelativeUrl: relative_url,
		PublishDate: publish_date,
	}
	if u := user.Current(c); u != nil {
		entry.Author = u.String()
	}
	_, err := datastore.Put(c, datastore.NewIncompleteKey(c, "Entries", nil), &entry)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	log.Printf("Saved new entry: %v", entry)
	http.Redirect(w, r, "/", http.StatusFound)
}
