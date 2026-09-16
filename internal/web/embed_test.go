package web

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestEmbeddedWebHandler(t *testing.T) {
	handler := Handler()

	paths := []string{
		"/",
		"/style.css",
		"/app.js",
		"/favicon.svg",
		"/pdfjs/pdf.min.js",
		"/pdfjs/pdf.worker.min.js",
		"/pdfjs/pdf_viewer.css",
	}

	for _, path := range paths {
		req := httptest.NewRequest("GET", path, nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("expected 200 OK for %s, got %d", path, w.Code)
		}
		if w.Body.Len() == 0 {
			t.Errorf("expected non-empty body for %s", path)
		}
	}

	// http.FileServer canonicalizes /index.html to /
	req := httptest.NewRequest("GET", "/index.html", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusMovedPermanently {
		t.Errorf("expected 301 Moved Permanently for /index.html canonicalization, got %d", w.Code)
	}
}
