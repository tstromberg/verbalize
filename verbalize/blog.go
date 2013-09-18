package blog

import (
	"appengine"
	"appengine/datastore"
	"appengine/user"
	"bytes"
	"fmt"
	"github.com/kylelemons/go-gypsy/yaml"
	"html/template"
	"log"
	"net/http"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	// Internal constants
	CONFIG_PATH = "verbalize.yml"
	VERSION     = "zero.20130915"
)

// Entry struct, stored in Datastore.
type Entry struct {
	Author   string
	IsHidden bool
	// Is this a page, or a blog entry?
	IsPage        bool
	AllowComments bool
	PublishDate   time.Time
	Title         string
	Content       []byte
	Slug          string
	RelativeURL   string
}

/* return a fetching key for a given entry */
func (e *Entry) Key(c appengine.Context) *datastore.Key {
	return datastore.NewKey(c, "Entries", e.Slug, 0, nil)
}

// Link struct, stored in Datastore.
type Link struct {
	Title string
	URL   string
	Order int64
}

/* return a fetching key for a given entry */
func (l *Link) Key(c appengine.Context) *datastore.Key {
	return datastore.NewKey(c, "Links", l.URL, 0, nil)
}

/* a mirror of Entry, with markings for raw HTML encoding. */
type EntryContext struct {
	Author         string
	IsHidden       bool
	IsPage         bool
	AllowComments  bool
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
	IsExcerpted    bool
	RelativeURL    string
	Slug           string
}

/* Entry.Context() generates template data from a stored entry */
func (e *Entry) Context() EntryContext {
	excerpt := bytes.SplitN(e.Content, []byte(config.Require("more_tag")),
		2)[0]

	return EntryContext{
		Author:         e.Author,
		IsHidden:       e.IsHidden,
		IsPage:         e.IsPage,
		AllowComments:  e.AllowComments,
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
		EscapedExcerpt: string(excerpt),
		IsExcerpted:    len(e.Content) != len(excerpt),
		RelativeURL:    e.RelativeURL,
		Slug:           e.Slug,
	}
}

/* This is sent to all templates */
type TemplateContext struct {
	SiteTitle       string
	SiteSubTitle    string
	SiteDescription string
	SiteTheme       string
	BaseURL         template.HTML

	Version string

	PageTitle       string
	PageId          string
	PageTimeRfc3339 string
	PageTimestamp   int64
	Entries         []EntryContext
	Links           []Link

	DisqusId              string
	GoogleAnalyticsId     string
	GoogleAnalyticsDomain string
	Hostname              template.HTML
}

/* Structure used for querying for blog entries */
type EntryQuery struct {
	Start         time.Time
	End           time.Time
	Count         int
	IncludeHidden bool
	IsPage        bool
	Tag           string // unused
}

// load and configure set of templates
func loadTemplate(paths ...string) *template.Template {
	t := template.New(strings.Join(paths, ","))
	t.Funcs(template.FuncMap{
		"eq": reflect.DeepEqual,
	})
	_, err := t.ParseFiles(paths...)
	if err != nil {
		panic(err)
	}
	return t
}

var (
	config = yaml.ConfigFile(CONFIG_PATH)

	theme_path      = filepath.Join("themes", config.Require("theme"))
	base_theme_path = filepath.Join(theme_path, "base.html")

	archiveTpl = loadTemplate(base_theme_path, filepath.Join(theme_path, "archive.html"))
	entryTpl   = loadTemplate(base_theme_path, filepath.Join(theme_path, "entry.html"))

	errorTpl = loadTemplate(base_theme_path, "templates/error.html")

	feedTpl = loadTemplate("templates/feed.html")

	adminEditTpl     = loadTemplate("templates/admin/base.html", "templates/admin/edit.html")
	adminHomeTpl     = loadTemplate("templates/admin/base.html", "templates/admin/home.html")
	adminPagesTpl    = loadTemplate("templates/admin/base.html", "templates/admin/pages.html")
	adminLinksTpl    = loadTemplate("templates/admin/base.html", "templates/admin/links.html")
	adminCommentsTpl = loadTemplate("templates/admin/base.html", "templates/admin/comments.html")

	// regexp matching an entry URL
	edit_entry_re = regexp.MustCompile(`edit/(?P<slug>[\w-]+)$`)
)

// Setup the URL handlers at initialization
func init() {
	/* ServeMux does not understand regular expressions :( */
	http.HandleFunc("/", rootHandler)
	http.HandleFunc("/feed/", feedHandler)

	http.HandleFunc("/admin", adminHomeHandler)
	http.HandleFunc("/admin/pages", adminPagesHandler)
	http.HandleFunc("/admin/edit", adminEditEntryHandler)
	http.HandleFunc("/admin/submit_entry", adminSubmitEntryHandler)
	http.HandleFunc("/admin/links", adminLinksHandler)
	http.HandleFunc("/admin/submit_links", adminSubmitLinksHandler)
	http.HandleFunc("/admin/comments", adminCommentsHandler)

}

// equality function for templates. Courtesy of Russ Cox
// https://groups.google.com/forum/#!topic/golang-nuts/OEdSDgEC7js
func equals(args ...interface{}) bool {
	if len(args) == 0 {
		return false
	}
	x := args[0]
	switch x := x.(type) {
	case string, int, int64, byte, float32, float64:
		for _, y := range args[1:] {
			if x == y {
				return true
			}
		}
		return false
	}

	for _, y := range args[1:] {
		if reflect.DeepEqual(x, y) {
			return true
		}
	}
	return false
}

// Render a named template name to the HTTP channel
func renderTemplate(w http.ResponseWriter, tmpl template.Template, context interface{}) {

	err := tmpl.ExecuteTemplate(w, "base.html", context)
	if err != nil {
		log.Printf("ERROR: %s", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func GetTemplateContext(entries []Entry, links []Link, pageTitle string,
	pageId string, r *http.Request) (
	t TemplateContext, err error) {
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

	/* These variables are optional. */
	disqus_id, _ := config.Get("disqus_id")
	google_analytics_id, _ := config.Get("google_analytics_id")
	google_analytics_domain, _ := config.Get("google_analytics_domain")

	t = TemplateContext{
		BaseURL:               template.HTML(base_url),
		SiteTitle:             config.Require("title"),
		SiteTheme:             config.Require("theme"),
		SiteSubTitle:          config.Require("subtitle"),
		SiteDescription:       config.Require("description"),
		Version:               VERSION,
		PageTimeRfc3339:       time.Now().Format(time.RFC3339),
		PageTimestamp:         time.Now().Unix() * 1000,
		Entries:               entry_contexts,
		Links:                 links,
		PageTitle:             pageTitle,
		PageId:                pageId,
		DisqusId:              disqus_id,
		GoogleAnalyticsId:     google_analytics_id,
		GoogleAnalyticsDomain: google_analytics_domain,
	}
	return t, err
}

// GetEntries retrieves all or some blog entries from datastore
func GetEntries(c appengine.Context, params EntryQuery) (entries []Entry, err error) {
	if params.Count == 0 {
		params.Count, _ = strconv.Atoi(config.Require("entries_per_page"))
	}
	q := datastore.NewQuery("Entries").Filter("IsPage = ", params.IsPage).Order(
		"-PublishDate").Limit(params.Count)

	if params.Start.IsZero() == false {
		q = q.Filter("PublishDate >", params.End)
	}
	if params.End.IsZero() == false {
		q = q.Filter("PublishDate <", params.End)
	}
	if params.IncludeHidden == false {
		q = q.Filter("IsHidden = ", false)
	}
	log.Printf("Query: %v", q)
	entries = make([]Entry, 0, params.Count)
	_, err = q.GetAll(c, &entries)
	return entries, err
}

// GetSingleEntry retrieves a single blog entry by slug from datastore
func GetSingleEntry(c appengine.Context, slug string) (e Entry, err error) {
	e.Slug = slug
	err = datastore.Get(c, e.Key(c), &e)
	return
}

// GetLinks retrieves all links in order from datastore
func GetLinks(c appengine.Context) (links []Link, err error) {
	q := datastore.NewQuery("Links").Order("Order").Order("Title")
	links = make([]Link, 0)
	_, err = q.GetAll(c, &links)
	return links, err
}

// HTTP handler for rendering blog entries
func rootHandler(w http.ResponseWriter, r *http.Request) {
	template := *errorTpl
	title := "Error"

	entries := make([]Entry, 0)
	c := appengine.NewContext(r)
	links, _ := GetLinks(c)

	if r.URL.Path == "/" {
		title = "Archive"
		template = *archiveTpl
		entries, _ = GetEntries(c, EntryQuery{IsPage: false})
	} else {
		entry, err := GetSingleEntry(c, filepath.Base(r.URL.Path))
		if err != nil {
			http.Error(w, "Nothing.", http.StatusNotFound)
		} else {
			title = entry.Title
			entries = append(entries, entry)
			template = *entryTpl
		}
	}
	context, _ := GetTemplateContext(entries, links, title, "root", r)
	renderTemplate(w, template, context)
}

// HTTP handler for /feed
func feedHandler(w http.ResponseWriter, r *http.Request) {
	log.Printf("%v", r.URL)
	c := appengine.NewContext(r)
	entries, _ := GetEntries(c, EntryQuery{IsPage: false})
	links := make([]Link, 0)

	context, _ := GetTemplateContext(entries, links, "Atom Feed", "feed", r)
	err := feedTpl.Execute(w, context)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// HTTP handler for /admin
func adminHomeHandler(w http.ResponseWriter, r *http.Request) {
	c := appengine.NewContext(r)
	entries, _ := GetEntries(c, EntryQuery{IncludeHidden: true, IsPage: false})
	context, _ := GetTemplateContext(entries, nil, "Home", "admin_home", r)
	renderTemplate(w, *adminHomeTpl, context)
}

// HTTP handler for /admin/pages
func adminPagesHandler(w http.ResponseWriter, r *http.Request) {
	c := appengine.NewContext(r)
	entries, _ := GetEntries(c, EntryQuery{IncludeHidden: true, IsPage: true})
	context, _ := GetTemplateContext(entries, nil, "Pages", "admin_pages", r)
	renderTemplate(w, *adminPagesTpl, context)
}

// HTTP handler for /edit
func adminEditEntryHandler(w http.ResponseWriter, r *http.Request) {
	slug := strings.TrimSpace(r.FormValue("slug"))
	is_page := strings.TrimSpace(r.FormValue("is_page"))

	c := appengine.NewContext(r)

	// just defaults.
	title := "New"
	entry := Entry{}

	if slug != "" {
		entry, err := GetSingleEntry(c, slug)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		title = entry.Title
	} else {
		if is_page != "" {
			entry.IsPage = true
		}
	}
	entries := make([]Entry, 0)
	entries = append(entries, entry)
	context, _ := GetTemplateContext(entries, nil, title, "admin_edit", r)
	renderTemplate(w, *adminEditTpl, context)
}

// HTTP handler for /admin/submit - submits a blog entry into datastore
func adminSubmitEntryHandler(w http.ResponseWriter, r *http.Request) {
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

	if r.FormValue("is_new_post") == "1" {
		if u := user.Current(c); u != nil {
			entry.Author = u.String()
		}
	} else {
		entry, _ = GetSingleEntry(c, slug)
	}

	entry.IsHidden, _ = strconv.ParseBool(r.FormValue("hidden"))
	entry.AllowComments, _ =  strconv.ParseBool(r.FormValue("allow_comments"))
	entry.IsPage, _ =  strconv.ParseBool(r.FormValue("is_page"))
	entry.Content = []byte(content)
	entry.Title = title
	entry.Slug = slug

	if entry.PublishDate.IsZero() {
		entry.PublishDate = time.Now()
	}

	if entry.IsPage {
		entry.RelativeURL = entry.Slug
	} else {
		entry.RelativeURL = fmt.Sprintf("%d/%02d/%s", entry.PublishDate.Year(),
			entry.PublishDate.Month(), entry.Slug)
	}
	_, err := datastore.Put(c, entry.Key(c), &entry)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	log.Printf("Saved entry: %v", entry)
	http.Redirect(w, r, fmt.Sprintf("/admin?added=%s", slug), http.StatusFound)
}

func adminLinksHandler(w http.ResponseWriter, r *http.Request) {
	c := appengine.NewContext(r)
	links, _ := GetLinks(c)
	context, _ := GetTemplateContext(nil, links, "Links", "admin_links", r)
	renderTemplate(w, *adminLinksTpl, context)
}

func adminSubmitLinksHandler(w http.ResponseWriter, r *http.Request) {
	c := appengine.NewContext(r)
	order, _ := strconv.Atoi(strings.TrimSpace(r.FormValue("new_order")))
	link := Link{
		Order: int64(order),
		Title: strings.TrimSpace(r.FormValue("new_title")),
		URL:   strings.TrimSpace(r.FormValue("new_url")),
	}
	_, err := datastore.Put(c, link.Key(c), &link)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	log.Printf("Saved entry: %v", link)
	http.Redirect(w, r, fmt.Sprintf("/admin/links?added=%s", link.URL), http.StatusFound)
}

func adminCommentsHandler(w http.ResponseWriter, r *http.Request) {
	context, _ := GetTemplateContext(nil, nil, "Comments", "admin_comments", r)
	renderTemplate(w, *adminCommentsTpl, context)
}
