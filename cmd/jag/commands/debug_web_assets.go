// Copyright (C) 2026 Toitware ApS. All rights reserved.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

package commands

import (
	"embed"
	"net/http"
	"strings"
)

//go:embed web/*
var webFS embed.FS

// serveIndex serves the embedded single-page UI and its static assets.
func serveIndex(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	if path == "/" {
		path = "/index.html"
	}
	data, err := webFS.ReadFile("web" + path)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	// The page and its assets are embedded in the jag binary and change with
	// every build; without this a browser can serve a stale cached app.js/css
	// after jag is rebuilt, so the UI silently runs old front-end code.
	w.Header().Set("Cache-Control", "no-store")
	switch {
	case strings.HasSuffix(path, ".html"):
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
	case strings.HasSuffix(path, ".css"):
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
	case strings.HasSuffix(path, ".js"):
		w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	}
	w.Write(data)
}
