package server

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/abjelosevic88/pocket-homelab-search/internal/read"
	"github.com/abjelosevic88/pocket-homelab-search/internal/store"
)

// ---- the reading view, pictures and site icons, all through the server ----

// proxied is the address of a picture through this app.
func proxied(raw string) string {
	if strings.HasPrefix(raw, "/") {
		return raw
	}
	return "/api/v1/img?u=" + url.QueryEscape(raw)
}

// handleRead: GET /api/v1/read?url= — the readable article of a page.
func (s *Server) handleRead(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	raw := strings.TrimSpace(r.URL.Query().Get("url"))
	if _, err := read.CheckURL(raw); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	st, _ := s.store.GetSettings(ctx, actorID(ctx))
	img := proxied
	if !st.ReaderImages {
		img = nil
	}
	if s.cfg.DemoData && s.engineName() == "demo" {
		writeJSON(w, http.StatusOK, demoArticle(raw, img != nil))
		return
	}
	rctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	a, err := s.fetch.Extract(rctx, raw, img)
	if err != nil {
		status := http.StatusBadGateway
		if errors.Is(err, read.ErrNoArticle) {
			status = http.StatusUnprocessableEntity
		}
		writeJSON(w, status, map[string]any{"error": err.Error(), "url": raw, "title": titleOr(a)})
		return
	}
	writeJSON(w, http.StatusOK, a)
}

func titleOr(a *read.Article) string {
	if a == nil {
		return ""
	}
	return a.Title
}

// imageHeaders: a picture opened on its own must never run as a page here.
func imageHeaders(w http.ResponseWriter, contentType string, maxAge int) {
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", fmt.Sprintf("private, max-age=%d", maxAge))
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; sandbox")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
}

// handleImage: GET /api/v1/img?u= — a picture fetched by the server.
func (s *Server) handleImage(w http.ResponseWriter, r *http.Request) {
	raw := r.URL.Query().Get("u")
	if strings.HasPrefix(raw, "/demo/img/") && s.cfg.DemoData {
		http.Redirect(w, r, raw, http.StatusFound)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	p, err := s.fetch.Image(ctx, raw)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	imageHeaders(w, p.ContentType, 86400)
	_, _ = w.Write(p.Body)
}

// handleFavicon: GET /api/v1/favicon?d=example.com — a site's icon, kept a
// month (a site without one is asked again after a day).
func (s *Server) handleFavicon(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	d := store.NormDomain(r.URL.Query().Get("d"))
	if d == "" || !strings.Contains(d, ".") || len(d) > 253 {
		writeErr(w, http.StatusBadRequest, "domain")
		return
	}
	if s.cfg.DemoData && s.engineName() == "demo" {
		imageHeaders(w, "image/svg+xml", 86400)
		_, _ = w.Write([]byte(demoFavicon(d)))
		return
	}
	key := "fav:" + d
	if v, at, ok := s.store.CacheGet(ctx, key); ok {
		t, _ := time.Parse(time.RFC3339Nano, at)
		fresh := time.Since(t) < 30*24*time.Hour
		if v == "" && time.Since(t) < 24*time.Hour {
			writeErr(w, http.StatusNotFound, "no icon")
			return
		}
		if v != "" && fresh {
			if ct, data, ok := strings.Cut(v, ";"); ok {
				if b, err := base64.StdEncoding.DecodeString(data); err == nil {
					imageHeaders(w, ct, 7*86400)
					_, _ = w.Write(b)
					return
				}
			}
		}
	}
	fctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	p, err := s.fetch.Favicon(fctx, d)
	if err != nil || len(p.Body) > 256<<10 {
		_ = s.store.CachePut(ctx, key, "")
		writeErr(w, http.StatusNotFound, "no icon")
		return
	}
	_ = s.store.CachePut(ctx, key, p.ContentType+";"+base64.StdEncoding.EncodeToString(p.Body))
	imageHeaders(w, p.ContentType, 7*86400)
	_, _ = w.Write(p.Body)
}
