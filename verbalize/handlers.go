package blog

import (
	"appengine"
	"appengine/datastore"
	"appengine/memcache"
	"appengine/user"
	"bytes"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"
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

// HTTP handler for rendering blog entries
func rootHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-control", config.Require("cache_control_header"))

	c := appengine.NewContext(r)
	key := r.URL.Path + "@" + appengine.VersionID(c)

	if item, err := memcache.Get(c, key); err == memcache.ErrCacheMiss {
		c.Infof("Page %s not in the cache", key)
	} else if err != nil {
		c.Errorf("error getting page: %v", err)
	} else {
		c.Infof("Page %s found in the cache", key)
		w.Write(item.Value)
		return
	}

	template := *errorTpl
	title := "Error"
	nextURL := ""
	previousURL := ""

	var entries []SavedEntry
	links, _ := GetLinks(c)
	path := r.URL.Path

	pageCount, _ := strconv.Atoi(filepath.Base(r.URL.Path))
	c.Infof("Page count: %d for %s", pageCount, r.URL.Path)
	if pageCount > 1 {
		path = filepath.Dir(path)
		if pageCount > 2 {
			previousURL = fmt.Sprintf("%s%d", path, pageCount-1)
		} else {
			// Don't link to /1, just link to the base path.
			previousURL = path
		}
	} else {
		pageCount = 1
	}

	if path == "/" {
		title = config.Require("subtitle")
		template = *archiveTpl
		entries_per_page, _ := config.GetInt("entries_per_page")
		offset := int(entries_per_page) * (pageCount - 1)
		c.Infof("Page %d - Entries Per page: %d - Offset: %d", pageCount, entries_per_page, offset)
		query := EntryQuery{
			IsPage: false,
			// We ask for 1 more than required so that we know if there are more links to show.
			Count:  int(entries_per_page) + 1,
			Offset: offset,
		}
		entries, _ = GetEntries(c, query)

		if len(entries) > int(entries_per_page) {
			nextURL = fmt.Sprintf("%s%d", path, pageCount+1)
			entries = entries[:entries_per_page]
		}

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
	context.PreviousURL = previousURL
	context.NextURL = nextURL

	var contentBuffer bytes.Buffer
	renderTemplate(&contentBuffer, template, context)
	content, err := ioutil.ReadAll(&contentBuffer)
	if err != nil {
		c.Errorf("Error reading content from buffer: %v", err)
	}
	w.Write(content)
	page_ttl, _ := config.GetInt("page_cache_ttl")
	storeInCache(c, key, content, int(page_ttl))
}

// HTTP handler for /feed
func feedHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-control", config.Require("cache_control_header"))

	c := appengine.NewContext(r)
	key := r.URL.Path + "@" + appengine.VersionID(c)

	if item, err := memcache.Get(c, key); err == memcache.ErrCacheMiss {
		c.Infof("Page %s not in the cache", key)
	} else if err != nil {
		c.Errorf("error getting page: %v", err)
	} else {
		c.Infof("Page %s found in the cache", key)
		w.Write(item.Value)
		return
	}

	entry_count, _ := config.GetInt("entries_per_page")
	entries, _ := GetEntries(c, EntryQuery{IsPage: false, Count: int(entry_count)})
	links := make([]SavedLink, 0)

	context, _ := GetTemplateContext(entries, links, "Atom Feed", "feed", r)
	var contentBuffer bytes.Buffer
	feedTpl.ExecuteTemplate(&contentBuffer, "feed.html", context)
	content, _ := ioutil.ReadAll(&contentBuffer)

	w.Write(content)
	// Feeds get cached infinitely, until an edit flushes it.
	storeInCache(c, key, content, 0)
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
	var entries []SavedEntry

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
		entry := SavedEntry{}
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
	entry := SavedEntry{}

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

// handler for /admin/links
func adminLinksHandler(w http.ResponseWriter, r *http.Request) {
	c := appengine.NewContext(r)
	links, _ := GetLinks(c)
	context, _ := GetTemplateContext(nil, links, "Links", "admin_links", r)
	renderTemplate(w, *adminLinksTpl, context)
}

// handler for /admin/submit_links
func adminSubmitLinksHandler(w http.ResponseWriter, r *http.Request) {
	c := appengine.NewContext(r)
	order, _ := strconv.Atoi(strings.TrimSpace(r.FormValue("new_order")))
	link := SavedLink{
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

// handler for /admin/comments
func adminCommentsHandler(w http.ResponseWriter, r *http.Request) {
	context, _ := GetTemplateContext(nil, nil, "Comments", "admin_comments", r)
	renderTemplate(w, *adminCommentsTpl, context)
}
