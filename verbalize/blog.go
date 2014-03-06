package blog

import (
	"appengine"
	"appengine/datastore"
	"bytes"
	"github.com/kylelemons/go-gypsy/yaml"
	"html/template"
	"io"
	"log"
	"net/http"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"time"
)

const (
	// Internal constants
	CONFIG_PATH = "verbalize.yml"
	VERSION     = "one.20140305"
)

var (
	config = yaml.ConfigFile(CONFIG_PATH)

	theme_path      = filepath.Join("themes", config.Require("theme"))
	base_theme_path = filepath.Join(theme_path, "base.html")

	archiveTpl       = loadTemplate(base_theme_path, filepath.Join(theme_path, "archive.html"))
	entryTpl         = loadTemplate(base_theme_path, filepath.Join(theme_path, "entry.html"))
	pageTpl          = loadTemplate(base_theme_path, filepath.Join(theme_path, "page.html"))
	errorTpl         = loadTemplate(base_theme_path, "templates/error.html")
	feedTpl          = loadTemplate("templates/feed.html")
	adminEditTpl     = loadTemplate("templates/admin/base.html", "templates/admin/edit.html")
	adminHomeTpl     = loadTemplate("templates/admin/base.html", "templates/admin/home.html")
	adminPagesTpl    = loadTemplate("templates/admin/base.html", "templates/admin/pages.html")
	adminLinksTpl    = loadTemplate("templates/admin/base.html", "templates/admin/links.html")
	adminCommentsTpl = loadTemplate("templates/admin/base.html", "templates/admin/comments.html")

	// regexp matching an entry URL
	edit_entry_re = regexp.MustCompile(`edit/(?P<slug>[\w-]+)$`)
)

/* All of the information we need to send about an entry to the template */
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
	Title          template.HTML
	Content        template.HTML
	Excerpt        template.HTML
	EscapedExcerpt string
	IsExcerpted    bool
	RelativeURL    string
	Slug           string
}

// Entry struct, stored in Datastore.
type SavedEntry struct {
	Author        string
	IsHidden      bool
	IsPage        bool
	AllowComments bool
	PublishDate   time.Time
	Title         string
	Content       []byte
	Slug          string
	RelativeURL   string
	// Unused: I haven't figured out how to delete this field from my tables yet.
	RelativeUrl string
}

/* return a fetching key for a given entry */
func (s *SavedEntry) Key(c appengine.Context) *datastore.Key {
	return datastore.NewKey(c, "Entries", s.Slug, 0, nil)
}

/* Entry.Context() generates template data from a stored entry */
func (s *SavedEntry) Context() EntryContext {
	excerpt := bytes.SplitN(s.Content, []byte(config.Require("more_tag")),
		2)[0]

	return EntryContext{
		Author:         s.Author,
		IsHidden:       s.IsHidden,
		IsPage:         s.IsPage,
		AllowComments:  s.AllowComments,
		Timestamp:      s.PublishDate.UTC().Unix(),
		Day:            s.PublishDate.Day(),
		Hour:           s.PublishDate.Hour(),
		Minute:         s.PublishDate.Minute(),
		Month:          s.PublishDate.Month(),
		MonthString:    s.PublishDate.Month().String(),
		Year:           s.PublishDate.Year(),
		RfcDate:        s.PublishDate.Format(time.RFC3339),
		Title:          template.HTML(s.Title),
		Content:        template.HTML(s.Content),
		Excerpt:        template.HTML(excerpt),
		EscapedExcerpt: string(excerpt),
		IsExcerpted:    len(s.Content) != len(excerpt),
		RelativeURL:    s.RelativeURL,
		Slug:           s.Slug,
	}
}

// Link struct, stored in Datastore.
type SavedLink struct {
	Title string
	URL   string
	Order int64
}

/* return a fetching key for a given entry */
func (sl *SavedLink) Key(c appengine.Context) *datastore.Key {
	return datastore.NewKey(c, "Links", sl.URL, 0, nil)
}

/* This is sent to all templates */
type GlobalTemplateContext struct {
	SiteTitle       template.HTML
	SiteSubTitle    template.HTML
	SiteDescription template.HTML
	SiteTheme       string
	BaseURL         template.HTML

	Version string
	Context appengine.Context

	PageTitle       string
	PageId          string
	PageTimeRfc3339 string
	PageTimestamp   int64
	Entries         []EntryContext
	Links           []SavedLink

	DisqusId              string
	GoogleAnalyticsId     string
	GoogleAnalyticsDomain string
	Hostname              template.HTML

	// If you would like to see more entries.
	NextURL     string
	PreviousURL string
}

/* Structure used for querying for blog entries */
type EntryQuery struct {
	Start         time.Time
	End           time.Time
	Count         int
	IncludeHidden bool
	IsPage        bool
	Tag           string // unused
	Offset        int
}

// load and configure set of templates
func loadTemplate(paths ...string) *template.Template {
	t := template.New(strings.Join(paths, ","))
	t.Funcs(template.FuncMap{
		"eq": reflect.DeepEqual,
		// see template_functions.go.
		"DaysUntil":          DaysUntil,
		"ExtractPageContent": ExtractPageContent,
	})
	_, err := t.ParseFiles(paths...)
	if err != nil {
		panic(err)
	}
	return t
}

// Render a named template name to the HTTP channel
func renderTemplate(w io.Writer, tmpl template.Template, context interface{}) {
	log.Printf("Rendering %s", tmpl.Name())
	err := tmpl.ExecuteTemplate(w, "base.html", context)
	if err != nil {
		log.Printf("ERROR: %s", err)
		w.Write([]byte("Unable to render"))
		return
	}
}

// GetTemplateContext creates a template context given a massive set of data.
func GetTemplateContext(entries []SavedEntry, links []SavedLink, pageTitle string, pageId string, r *http.Request) (t GlobalTemplateContext, err error) {
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

	c := appengine.NewContext(r)

	t = GlobalTemplateContext{
		BaseURL:               template.HTML(base_url),
		SiteTitle:             template.HTML(config.Require("title")),
		SiteTheme:             config.Require("theme"),
		SiteSubTitle:          template.HTML(config.Require("subtitle")),
		SiteDescription:       template.HTML(config.Require("description")),
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
		Context:               c,
	}
	return t, err
}

// GetEntries retrieves all or some blog entries from datastore
func GetEntries(c appengine.Context, params EntryQuery) (entries []SavedEntry, err error) {
	q := datastore.NewQuery("Entries").Order(
		"-PublishDate")

	if params.Count > 0 {
		q = q.Limit(params.Count)
	}
	if params.IsPage == false {
		q = q.Filter("IsPage =", false)
	}
	if params.IsPage == true {
		q = q.Filter("IsPage =", true)
	}
	if params.Start.IsZero() == false {
		q = q.Filter("PublishDate >", params.End)
	}
	if params.End.IsZero() == false {
		q = q.Filter("PublishDate <", params.End)
	}
	if params.IncludeHidden == false {
		q = q.Filter("IsHidden = ", false)
	}
	if params.Offset > 0 {
		q = q.Offset(params.Offset)
	}
	log.Printf("Query: %v", q)

	_, err = q.GetAll(c, &entries)
	return entries, err
}

// GetSingleEntry retrieves a single blog entry by slug from datastore
func GetSingleEntry(c appengine.Context, slug string) (e SavedEntry, err error) {
	e.Slug = slug
	err = datastore.Get(c, e.Key(c), &e)
	return
}

// GetLinks retrieves all links in order from datastore
func GetLinks(c appengine.Context) (links []SavedLink, err error) {
	q := datastore.NewQuery("Links").Order("Order").Order("Title")
	_, err = q.GetAll(c, &links)
	return links, err
}
