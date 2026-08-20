package main

import (
	"net/url"
	"strings"
)

// decodeMediaPath turns a request path/query value into a relative media path.
// r.URL.Query().Get and gorilla/mux already unescape %XX sequences. A second
// url.QueryUnescape is wrong: it treats '+' as space, so a folder like
// "[BD+ESPECIAIS]" becomes "[BD ESPECIAIS]" and the lookup 404s.
func decodeMediaPath(raw string) string {
	if raw == "" || !strings.Contains(raw, "%") {
		return raw
	}
	decoded, err := url.PathUnescape(raw)
	if err != nil {
		return raw
	}
	return decoded
}
