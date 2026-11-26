package store

import (
	"net/url"
	"path/filepath"
	"strings"
)

func sqliteURI(path, query string) string {
	path = filepath.ToSlash(path)
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	u := url.URL{Scheme: "file", Path: path, RawQuery: query}
	return u.String()
}
