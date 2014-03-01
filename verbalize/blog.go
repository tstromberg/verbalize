package blog

import (
	"appengine"
	"appengine/datastore"
	"appengine/memcache"
	"appengine/urlfetch"
	"appengine/user"
	"bufio"
	"bytes"
	"fmt"
	"github.com/kylelemons/go-gypsy/yaml"
	"html/template"
	"io"
	"io/ioutil"
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
	VERSION     = "zero.20140301"
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
	// Compatibility
	RelativeUrl string
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
	Title          template.HTML
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
		Title:          template.HTML(e.Title),
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

// DaysUntil returns the number of days until a date - used for templates.
func DaysUntil(dateStr string) (days int, err error) {
	target, err := time.Parse("1/2/2006", dateStr)
	if err != nil {
		return -1, err
	}
	duration := target.Sub(time.Now())
	days = int(duration.Seconds() / 86400)
	return
}

// ExtractPage extracts content from a given URL - used for templates.
func ExtractPageContent(c appengine.Context, URL, start_token, end_token string) (content template.HTML, err error) {
	key := fmt.Sprintf("%s-%s-%s", URL, start_token, end_token)

	if item, err := memcache.Get(c, key); err == memcache.ErrCacheMiss {
		c.Infof("URL %s not in the cache", URL)
	} else if err != nil {
		c.Errorf("error getting URL: %v", err)
	} else {
		c.Infof("Returning cached contents of  %s", URL)
		return template.HTML(item.Value), nil
	}

	c.Infof("Key %s not in the cache - fetching!", key)
	client := urlfetch.Client(c)
	resp, err := client.Get(URL)
	if err != nil {
		c.Errorf("error fetching %s: %v", URL, err)
		return "", err
	}
	scanner := bufio.NewScanner(resp.Body)
	defer resp.Body.Close()
	inBlock := false
	buffer := new(bytes.Buffer)

	c.Infof("Scanning URL content for start=%s end=%s", start_token, end_token)
	for scanner.Scan() {
		// TODO(tstromberg): Use something that doesn't depend on newlines.
		if strings.Contains(scanner.Text(), start_token) {
			inBlock = true
		}
		if strings.Contains(scanner.Text(), end_token) {
			inBlock = false
		}
		if inBlock == true {
			buffer.Write(scanner.Bytes())
		}
	}

	if err := scanner.Err(); err != nil {
		return "", err
	}

	extract := buffer.String()
	cacheOutput(c, key, []byte(extract), external_page_ttl)
	return template.HTML(extract), nil
}

// load and configure set of templates
func loadTemplate(paths ...string) *template.Template {
	t := template.New(strings.Join(paths, ","))
	t.Funcs(template.FuncMap{
		"eq":                 reflect.DeepEqual,
		"DaysUntil":          DaysUntil,
		"ExtractPageContent": ExtractPageContent,
	})
	_, err := t.ParseFiles(paths...)
	if err != nil {
		panic(err)
	}
	return t
}

var (
	config               = yaml.ConfigFile(CONFIG_PATH)
	external_page_ttl, _ = strconv.Atoi(config.Require("external_page_cache_ttl"))
	page_ttl, _          = strconv.Atoi(config.Require("page_cache_ttl"))

	theme_path      = filepath.Join("themes", config.Require("theme"))
	base_theme_path = filepath.Join(theme_path, "base.html")

	archiveTpl = loadTemplate(base_theme_path, filepath.Join(theme_path, "archive.html"))
	entryTpl   = loadTemplate(base_theme_path, filepath.Join(theme_path, "entry.html"))
	pageTpl    = loadTemplate(base_theme_path, filepath.Join(theme_path, "page.html"))

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
	http.HandleFunc("/admin/home", adminHomeHandler)
	http.HandleFunc("/admin/pages", adminPagesHandler)
	http.HandleFunc("/admin/edit", adminEditEntryHandler)
	http.HandleFunc("/admin/submit_entry", adminSubmitEntryHandler)
	http.HandleFunc("/admin/links", adminLinksHandler)
	http.HandleFunc("/admin/submit_links", adminSubmitLinksHandler)
	http.HandleFunc("/admin/comments", adminCommentsHandler)

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

	c := appengine.NewContext(r)

	t = TemplateContext{
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
func GetEntries(c appengine.Context, params EntryQuery) (entries []Entry, err error) {
	if params.Count == 0 {
		params.Count, _ = strconv.Atoi(config.Require("entries_per_page"))
	}
	q := datastore.NewQuery("Entries").Order(
		"-PublishDate").Limit(params.Count)

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
	c := appengine.NewContext(r)
	if item, err := memcache.Get(c, r.URL.Path); err == memcache.ErrCacheMiss {
		c.Infof("Page %s not in the cache", r.URL.Path)
	} else if err != nil {
		c.Errorf("error getting page: %v", err)
	} else {
		c.Infof("Page %s found in the cache", r.URL.Path)
		w.Write(item.Value)
		return
	}

	template := *errorTpl
	title := "Error"
	entries := make([]Entry, 0)
	links, _ := GetLinks(c)

	if r.URL.Path == "/" {
		title = config.Require("subtitle")
		template = *archiveTpl
		entries, _ = GetEntries(c, EntryQuery{IsPage: false})
	} else {
		entry, err := GetSingleEntry(c, filepath.Base(r.URL.Path))
		if err != nil {
			http.Error(w, "I looked for an entry, but it was not there.", http.StatusNotFound)
			return
		} else {
			title = entry.Title
			entries = append(entries, entry)
			if entry.IsPage == true {
				template = *pageTpl
			} else {
				template = *entryTpl
			}
		}
	}
	context, _ := GetTemplateContext(entries, links, title, "root", r)
	var contentBuffer bytes.Buffer
	renderTemplate(&contentBuffer, template, context)
	content, err := ioutil.ReadAll(&contentBuffer)
	if err != nil {
		c.Errorf("Error reading content from buffer: %v", err)
	}
	w.Write(content)
	cacheOutput(c, r.URL.Path, content, page_ttl)
}

// HTTP handler for /feed
func feedHandler(w http.ResponseWriter, r *http.Request) {
	c := appengine.NewContext(r)
	if item, err := memcache.Get(c, r.URL.Path); err == memcache.ErrCacheMiss {
		c.Infof("Page %s not in the cache", r.URL.Path)
	} else if err != nil {
		c.Errorf("error getting page: %v", err)
	} else {
		c.Infof("Page %s found in the cache", r.URL.Path)
		w.Write(item.Value)
		return
	}

	entries, _ := GetEntries(c, EntryQuery{IsPage: false})
	links := make([]Link, 0)

	context, _ := GetTemplateContext(entries, links, "Atom Feed", "feed", r)
	var contentBuffer bytes.Buffer
	feedTpl.ExecuteTemplate(&contentBuffer, "feed.html", context)
	content, _ := ioutil.ReadAll(&contentBuffer)
	w.Write(content)
	cacheOutput(c, r.URL.Path, content, page_ttl)
}

// Duplicated code for caching the output of something.
func cacheOutput(c appengine.Context, key string, content []byte, ttl int) error {
	item := &memcache.Item{
		Key:        key,
		Value:      content,
		Expiration: time.Duration(ttl) * time.Second,
	}
	c.Infof("Caching contents of %s for %s", item.Key, item.Expiration)
	err := memcache.Add(c, item)
	if err != nil {
		c.Errorf("error adding %v to cache: %v", item, err)
	}
	return err
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
	log.Printf("Edit: %s (%s)", slug, is_page)

	c := appengine.NewContext(r)

	// just defaults.
	title := "New"
	entries := make([]Entry, 0)

	if slug != "" {
		entry, err := GetSingleEntry(c, slug)
		log.Printf("GetSingleEntry: %s / %s", entry, err)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		entries = append(entries, entry)
		title = entry.Title
	} else {
		entry := Entry{}
		if is_page != "" {
			entry.IsPage = true
			entry.AllowComments = false
		} else {
			entry.AllowComments = true
			entry.IsPage = false
		}
		entries = append(entries, entry)
	}

	log.Printf("Entries: %v", entries)
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

	if r.FormValue("hidden") == "on" {
		entry.IsHidden = true
	} else {
		entry.IsHidden = false
	}
	if r.FormValue("allow_comments") == "on" {
		entry.AllowComments = true
	} else {
		entry.AllowComments = false
	}
	entry.IsPage, _ = strconv.ParseBool(r.FormValue("is_page"))
	entry.Content = []byte(content)
	entry.Title = title
	entry.Slug = slug
	log.Printf("Comments: %s (real=%s)", entry.AllowComments, r.FormValue("allow_comments"))
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
	memcache.Flush(c)
	if entry.IsPage {
		http.Redirect(w, r, fmt.Sprintf("/admin/pages?added=%s", slug), http.StatusFound)
	} else {
		http.Redirect(w, r, fmt.Sprintf("/admin?added=%s", slug), http.StatusFound)
	}
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
	memcache.Flush(c)
	http.Redirect(w, r, fmt.Sprintf("/admin/links?added=%s", link.URL), http.StatusFound)
}

func adminCommentsHandler(w http.ResponseWriter, r *http.Request) {
	context, _ := GetTemplateContext(nil, nil, "Comments", "admin_comments", r)
	renderTemplate(w, *adminCommentsTpl, context)
}
