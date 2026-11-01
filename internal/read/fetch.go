// Package read fetches web pages on a person's behalf: the readable article
// of a page (the reading view), pictures and site icons. Every request comes
// from the server, so the site sees the server and never the person's
// device, cookies or address. Private networks are refused unless allowed.
package read

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"syscall"
	"time"
)

// ErrBlocked is returned for addresses on a private network.
var ErrBlocked = errors.New("that address is on a private network (an admin can allow it with FETCH_PRIVATE=true)")

// Fetcher downloads pages and pictures.
type Fetcher struct {
	UserAgent    string
	AllowPrivate bool
	client       *http.Client
}

// NewFetcher returns a fetcher.
func NewFetcher(userAgent string, allowPrivate bool) *Fetcher {
	f := &Fetcher{UserAgent: userAgent, AllowPrivate: allowPrivate}
	d := &net.Dialer{Timeout: 8 * time.Second}
	if !allowPrivate {
		d.Control = func(network, address string, _ syscall.RawConn) error {
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				return err
			}
			if ip := net.ParseIP(host); ip != nil && privateIP(ip) {
				return ErrBlocked
			}
			return nil
		}
	}
	t := &http.Transport{DialContext: d.DialContext, TLSHandshakeTimeout: 8 * time.Second, ResponseHeaderTimeout: 15 * time.Second,
		MaxIdleConns: 32, MaxIdleConnsPerHost: 4, IdleConnTimeout: 60 * time.Second, ForceAttemptHTTP2: true}
	f.client = &http.Client{Transport: t, Timeout: 25 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 8 {
			return errors.New("too many redirects")
		}
		if req.URL.Scheme != "http" && req.URL.Scheme != "https" {
			return errors.New("redirected away from the web")
		}
		return nil
	}}
	return f
}

var cgnat = &net.IPNet{IP: net.IPv4(100, 64, 0, 0), Mask: net.CIDRMask(10, 32)}

func privateIP(ip net.IP) bool {
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() || ip.IsMulticast() || cgnat.Contains(ip)
}

// CheckURL accepts http(s) addresses only.
func CheckURL(raw string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, errors.New("not a web address (it needs http:// or https://)")
	}
	return u, nil
}

// Page is a downloaded body.
type Page struct {
	Body        []byte
	ContentType string
	URL         string // after redirects
}

func (f *Fetcher) get(ctx context.Context, raw, accept string, limit int64) (*Page, error) {
	u, err := CheckURL(raw)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", f.UserAgent)
	req.Header.Set("Accept", accept)
	req.Header.Set("Accept-Language", "en;q=0.9,*;q=0.5")
	res, err := f.client.Do(req)
	if err != nil {
		if errors.Is(err, ErrBlocked) {
			return nil, ErrBlocked
		}
		return nil, friendly(err)
	}
	defer res.Body.Close()
	if res.StatusCode >= 400 {
		switch res.StatusCode {
		case 401, 403:
			return nil, fmt.Errorf("the site refused to show this page to a server (HTTP %d); open the original instead", res.StatusCode)
		case 404, 410:
			return nil, fmt.Errorf("the page is gone (HTTP %d)", res.StatusCode)
		}
		return nil, fmt.Errorf("the site answered HTTP %d", res.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(res.Body, limit))
	if err != nil {
		return nil, friendly(err)
	}
	return &Page{Body: b, ContentType: res.Header.Get("Content-Type"), URL: res.Request.URL.String()}, nil
}

func friendly(err error) error {
	var dns *net.DNSError
	switch {
	case errors.As(err, &dns):
		return fmt.Errorf("the address %s does not resolve", dns.Name)
	case errors.Is(err, context.DeadlineExceeded), strings.Contains(err.Error(), "Client.Timeout"):
		return errors.New("the page took too long to answer")
	case strings.Contains(err.Error(), "connection refused"):
		return errors.New("the site refused the connection")
	case strings.Contains(err.Error(), "certificate"):
		return errors.New("the site's certificate is not valid")
	}
	return err
}

// MaxImage is the largest picture passed through.
const MaxImage = 12 << 20

// Image downloads a picture (only pictures: anything else is refused, so the
// proxy cannot be used to fetch pages).
func (f *Fetcher) Image(ctx context.Context, raw string) (*Page, error) {
	p, err := f.get(ctx, raw, "image/avif,image/webp,image/png,image/svg+xml,image/*;q=0.8", MaxImage)
	if err != nil {
		return nil, err
	}
	mt, _, _ := mime.ParseMediaType(p.ContentType)
	if !strings.HasPrefix(mt, "image/") {
		mt = http.DetectContentType(p.Body)
		if !strings.HasPrefix(mt, "image/") {
			return nil, errors.New("not a picture")
		}
	}
	p.ContentType = mt
	return p, nil
}

var reIcon = regexp.MustCompile(`(?is)<link\b[^>]*\brel=["']?(?:shortcut )?(?:icon|apple-touch-icon)["']?[^>]*>`)
var reHref = regexp.MustCompile(`(?is)\bhref=["']?([^"' >]+)`)

// Favicon finds a site's icon: /favicon.ico, else the icon the home page names.
func (f *Fetcher) Favicon(ctx context.Context, domain string) (*Page, error) {
	if p, err := f.Image(ctx, "https://"+domain+"/favicon.ico"); err == nil && len(p.Body) > 0 {
		return p, nil
	}
	home, err := f.get(ctx, "https://"+domain+"/", "text/html", 512<<10)
	if err != nil {
		return nil, err
	}
	base, _ := url.Parse(home.URL)
	for _, tag := range reIcon.FindAll(home.Body, 8) {
		if m := reHref.FindSubmatch(tag); m != nil {
			if ref, err := url.Parse(string(bytes.TrimSpace(m[1]))); err == nil && base != nil {
				if p, err := f.Image(ctx, base.ResolveReference(ref).String()); err == nil {
					return p, nil
				}
			}
		}
	}
	return nil, errors.New("no icon")
}
