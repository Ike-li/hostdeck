// Package local keeps the console and the services it embeds to requests
// addressed to this machine.
package local

import (
	"net"
	"net/http"
)

// Only answers requests addressed to 127.0.0.1, so a web page that rebinds its
// own domain to 127.0.0.1 cannot read what is behind it. localhost is sent to
// 127.0.0.1: they are different sites to the browser, and keeping one of them
// keeps one set of cookies and storage for each embedded service.
func Only(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, port, err := net.SplitHostPort(r.Host)
		if err != nil {
			host, port = r.Host, ""
		}
		switch host {
		case "127.0.0.1":
			next.ServeHTTP(w, r)
		case "localhost":
			to := "http://127.0.0.1"
			if port != "" {
				to += ":" + port
			}
			http.Redirect(w, r, to+r.URL.RequestURI(), http.StatusTemporaryRedirect)
		default:
			http.Error(w, "hostdeck answers requests to 127.0.0.1 only", http.StatusForbidden)
		}
	})
}
