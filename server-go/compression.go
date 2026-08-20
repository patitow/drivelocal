package main

import (
	"compress/gzip"
	"io"
	"net/http"
	"strings"
)

// gzipResponseWriter wraps http.ResponseWriter to add gzip compression
type gzipResponseWriter struct {
	io.Writer
	http.ResponseWriter
}

func (w *gzipResponseWriter) Write(b []byte) (int, error) {
	return w.Writer.Write(b)
}

// compressionMiddleware adds gzip compression to responses
func compressionMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Skip compression for already compressed content or binary files
		if strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			// Don't compress images, videos, or already compressed content
			contentType := r.Header.Get("Content-Type")
			path := r.URL.Path
			
			shouldCompress := true
			if strings.HasPrefix(path, "/api/thumbnail/") ||
				strings.HasPrefix(path, "/api/stream/") ||
				strings.HasPrefix(path, "/api/download/") ||
				strings.Contains(contentType, "image/") ||
				strings.Contains(contentType, "video/") ||
				strings.Contains(contentType, "application/zip") ||
				strings.Contains(contentType, "application/octet-stream") {
				shouldCompress = false
			}

			if shouldCompress {
				w.Header().Set("Content-Encoding", "gzip")
				w.Header().Set("Vary", "Accept-Encoding")
				
				gz := gzip.NewWriter(w)
				defer gz.Close()
				
				gzw := &gzipResponseWriter{Writer: gz, ResponseWriter: w}
				next.ServeHTTP(gzw, r)
				return
			}
		}
		
		next.ServeHTTP(w, r)
	})
}

