// Package proxy re-serves a local web UI on its own port so that the console
// may frame it.
package proxy

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"

	"github.com/Ike-li/hostdeck/internal/local"
)

// New forwards every request but ClearPath and NotificationsPath to target's
// scheme and host. In the responses, only the service itself and the console
// may frame it; the rest of the service's Content-Security-Policy passes
// through untouched. redeem says whether a token for clearing this service is
// one the console handed out.
// ClearPath is where the proxy, not the service, answers with Clear-Site-Data.
const ClearPath = "/.well-known/hostdeck/clear-site-data"

// NotificationsPath is where the proxy, not the service, answers with a page
// that asks the browser to let this port send notifications.
const NotificationsPath = "/.well-known/hostdeck/notifications"

func New(target *url.URL, consoleOrigin string, redeem func(token string) bool) http.Handler {
	base := &url.URL{Scheme: target.Scheme, Host: target.Host}
	// 'self' keeps working the pages that a service frames inside its own
	// pages: frame-ancestors checks every ancestor, the console included.
	ancestors := "frame-ancestors 'self' " + consoleOrigin
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.DialContext = dialLocal
	rp := &httputil.ReverseProxy{
		Transport: transport,
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
			clearSiteData(w, r, redeem, consoleOrigin)
			return
		}
		if r.URL.Path == NotificationsPath {
			askForNotifications(w, r, consoleOrigin)
			return
		}
		rp.ServeHTTP(w, r)
	}))
}

// dialLocal resolves addr's host and connects only when every address it stands
// for is loopback, private or link-local, so a host name that later points to
// the internet is never proxied.
func dialLocal(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	lookupCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	addrs, err := net.DefaultResolver.LookupNetIP(lookupCtx, "ip", host)
	if err != nil {
		return nil, err
	}
	if len(addrs) == 0 {
		return nil, fmt.Errorf("%s resolved to no addresses", host)
	}
	for _, a := range addrs {
		if a = a.Unmap(); !a.IsLoopback() && !a.IsPrivate() && !a.IsLinkLocalUnicast() {
			return nil, fmt.Errorf("%s is not on this machine or the local network", host)
		}
	}
	var d net.Dialer
	var lastErr error
	for _, a := range addrs {
		conn, err := d.DialContext(ctx, network, net.JoinHostPort(a.Unmap().String(), port))
		if err == nil {
			return conn, nil
		}
		lastErr = err
	}
	return nil, lastErr
}

// clearSiteData has the browser drop the cache and storage it keeps for this
// service's origin. Cookies stay: the browser keeps them per host, not per
// port, so clearing them would sign the user out of every service. A token
// from the console is required, because every other page on 127.0.0.1 is the
// same site and could otherwise wipe a service's data with a link. The page
// tells the console that it cleared: a refusal also loads, so loading alone
// does not say so.
func clearSiteData(w http.ResponseWriter, r *http.Request, redeem func(string) bool, consoleOrigin string) {
	if r.Method != http.MethodGet || !redeem(r.URL.Query().Get("token")) {
		http.Error(w, "clearing needs a token from the console", http.StatusForbidden)
		return
	}
	w.Header().Set("Clear-Site-Data", `"cache", "storage"`)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	origin, _ := json.Marshal(consoleOrigin)
	fmt.Fprintf(w, "<!doctype html><title>Cleared</title><script>parent.postMessage('hostdeck:cleared', %s)</script>", origin)
}

// askForNotifications answers the small window the console opens on this
// port. A browser keys a notification permission by the top-level page's
// origin, so a service framed by the console cannot be asked from inside its
// frame, and its service worker, which runs without the console, needs this
// port's own permission. The page asks on a click, tells the console what the
// browser answered and closes; an answer given before closes it at once. A
// blocked address cannot be asked again from a page, so then the window stays,
// says where the browser keeps the setting, and closes once it is turned on.
// It writes nothing, so it takes no token.
func askForNotifications(w http.ResponseWriter, r *http.Request, consoleOrigin string) {
	text := notifyEN
	if r.URL.Query().Get("lang") == "zh-CN" {
		text = notifyZH
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	origin, _ := json.Marshal(consoleOrigin)
	fmt.Fprintf(w, notifyPage, text.lang, text.title, text.title, text.hint, text.blocked, text.allow, origin)
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

var (
	notifyEN = struct{ lang, title, hint, blocked, allow string }{"en", "Notifications from this service", "Your browser asks once for this address. This window closes when you answer.", "Your browser has blocked notifications for this address. Click the icon to the left of the address bar and turn on Notifications; this window then closes by itself.", "Allow notifications"}
	notifyZH = struct{ lang, title, hint, blocked, allow string }{"zh-CN", "这个服务的通知", "浏览器会为这个地址询问一次，回答后这个窗口会自动关闭。", "浏览器已阻止这个地址的通知。点地址栏左边的图标，打开「通知」，这个窗口随后会自动关闭。", "允许通知"}
)

const notifyPage = `<!doctype html><html lang="%s"><meta charset="utf-8"><meta name="color-scheme" content="light dark"><title>%s</title>
<body style="font: 15px/1.6 -apple-system, 'Segoe UI', 'PingFang SC', 'Microsoft YaHei', sans-serif; margin: 0; padding: 24px">
<p style="font-size: 1.2em; margin: 0 0 .4em">%s</p>
<p id="hint" style="opacity: .7; margin: 0 0 1em">%s</p>
<p id="blocked" hidden style="margin: 0">%s</p>
<button id="allow" type="button" style="font: inherit; min-height: 44px; padding: 0 16px">%s</button>
<script>
const allow = document.getElementById('allow'), hint = document.getElementById('hint'), blocked = document.getElementById('blocked');
const settle = (permission) => {
  if (opener) opener.postMessage({ type: 'hostdeck:notifications', permission }, %s);
  if (permission !== 'denied') return close();
  allow.hidden = hint.hidden = true;
  blocked.hidden = false;
};
if (!('Notification' in window)) settle('unsupported');
else {
  navigator.permissions.query({ name: 'notifications' }).then((status) => {
    status.onchange = () => { if (Notification.permission !== 'denied') settle(Notification.permission); };
  });
  if (Notification.permission !== 'default') settle(Notification.permission);
  else allow.onclick = () => Notification.requestPermission().then(settle);
}
</script>`

const unreachablePage = `<!doctype html><html lang="%s"><meta charset="utf-8"><meta name="color-scheme" content="light dark">
<body style="font: 15px/1.6 -apple-system, 'Segoe UI', 'PingFang SC', 'Microsoft YaHei', sans-serif; display: grid; place-items: center; height: 90vh; margin: 0">
<div style="max-width: 32em; padding: 0 1.5em"><p style="font-size: 1.2em; margin: 0 0 .4em">%s</p>
<p style="opacity: .7; margin: 0">%s</p>
<p style="opacity: .45; font-size: .85em">%s</p></div>`
