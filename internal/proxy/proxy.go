// Package proxy re-serves a local web UI on its own port so that the console
// may frame it.
package proxy

import (
	"fmt"
	"html"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"

	"github.com/Ike-li/hostdeck/internal/local"
)

// New forwards every request but ClearPath to target's scheme and host. In the
// responses, only the service itself and the console may frame it; the rest of
// the service's Content-Security-Policy passes through untouched. redeem says
// whether a token for clearing this service is one the console handed out.
// ClearPath is where the proxy, not the service, answers with Clear-Site-Data.
const ClearPath = "/.well-known/hostdeck/clear-site-data"

func New(target *url.URL, consoleOrigin string, redeem func(token string) bool) http.Handler {
	base := &url.URL{Scheme: target.Scheme, Host: target.Host}
	// 'self' keeps working the pages that a service frames inside its own
	// pages: frame-ancestors checks every ancestor, the console included.
	ancestors := "frame-ancestors 'self' " + consoleOrigin
	rp := &httputil.ReverseProxy{
		Rewrite: func(r *httputil.ProxyRequest) {
			r.SetURL(base)
			// Services such as Grafana refuse requests whose Origin and Host
			// disagree, and the browser's Origin is this proxy's.
			r.Out.Host = r.In.Host
		},
		ModifyResponse: func(resp *http.Response) error {
			resp.Header.Del("X-Frame-Options")
			var csp []string
			for _, policy := range resp.Header.Values("Content-Security-Policy") {
				if policy = dropDirective(policy, "frame-ancestors"); policy != "" {
					csp = append(csp, policy)
				}
			}
			resp.Header["Content-Security-Policy"] = append(csp, ancestors)
			return nil
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			text := unreachableEN
			if prefersChinese(r.Header.Get("Accept-Language")) {
				text = unreachableZH
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusBadGateway)
			fmt.Fprintf(w, unreachablePage, text.lang, html.EscapeString(fmt.Sprintf(text.title, target.Host)), text.hint, html.EscapeString(err.Error()))
		},
	}
	return local.Only(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == ClearPath {
			clearSiteData(w, r, redeem)
			return
		}
		rp.ServeHTTP(w, r)
	}))
}

// clearSiteData has the browser drop the cache and storage it keeps for this
// service's origin. Cookies stay: the browser keeps them per host, not per
// port, so clearing them would sign the user out of every service. A token
// from the console is required, because every other page on 127.0.0.1 is the
// same site and could otherwise wipe a service's data with a link.
func clearSiteData(w http.ResponseWriter, r *http.Request, redeem func(string) bool) {
	if r.Method != http.MethodGet || !redeem(r.URL.Query().Get("token")) {
		http.Error(w, "clearing needs a token from the console", http.StatusForbidden)
		return
	}
	w.Header().Set("Clear-Site-Data", `"cache", "storage"`)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	io.WriteString(w, "<!doctype html><title>Cleared</title>")
}

func dropDirective(policy, name string) string {
	var kept []string
	for _, d := range strings.Split(policy, ";") {
		fields := strings.Fields(d)
		if len(fields) == 0 || strings.EqualFold(fields[0], name) {
			continue
		}
		kept = append(kept, strings.Join(fields, " "))
	}
	return strings.Join(kept, "; ")
}

// prefersChinese reads Accept-Language in the browser's order of preference.
// The page shows inside the console, which cannot tell the proxy its own choice.
func prefersChinese(accept string) bool {
	for _, tag := range strings.Split(accept, ",") {
		tag, _, _ = strings.Cut(tag, ";")
		tag = strings.ToLower(strings.TrimSpace(tag))
		switch {
		case strings.HasPrefix(tag, "zh"):
			return true
		case strings.HasPrefix(tag, "en"):
			return false
		}
	}
	return false
}

var (
	unreachableEN = struct{ lang, title, hint string }{"en", "Cannot reach %s", "The service may not be running. Start it, then press Reload above."}
	unreachableZH = struct{ lang, title, hint string }{"zh-CN", "连不上 %s", "服务可能没在运行。启动它之后，点上方的「刷新」。"}
)

const unreachablePage = `<!doctype html><html lang="%s"><meta charset="utf-8"><meta name="color-scheme" content="light dark">
<body style="font: 15px/1.6 -apple-system, 'Segoe UI', 'PingFang SC', 'Microsoft YaHei', sans-serif; display: grid; place-items: center; height: 90vh; margin: 0">
<div style="max-width: 32em; padding: 0 1.5em"><p style="font-size: 1.2em; margin: 0 0 .4em">%s</p>
<p style="opacity: .7; margin: 0">%s</p>
<p style="opacity: .45; font-size: .85em">%s</p></div>`
