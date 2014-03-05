// This is a set of custom functions exported to templates.
package blog

import (
	"appengine"
	"appengine/memcache"
	"appengine/urlfetch"
	"bufio"
	"bytes"
	"fmt"
	"html/template"
	"strings"
	"time"
)

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
	external_page_ttl, _ := config.GetInt("external_page_cache_ttl")
	storeInCache(c, key, []byte(extract), int(external_page_ttl))
	return template.HTML(extract), nil
}
