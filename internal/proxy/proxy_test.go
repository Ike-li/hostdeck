package proxy

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const consoleOrigin = "http://127.0.0.1:4678"

func refuseAll(string) bool { return false }

// serveThroughProxy starts upstream and a proxy in front of it whose target
// carries path, and returns the proxy's URL.
func serveThroughProxy(t *testing.T, path string, upstream http.HandlerFunc) string {
	t.Helper()
	up := httptest.NewServer(upstream)
	t.Cleanup(up.Close)
	target, _ := url.Parse(up.URL + path)
	proxy := httptest.NewServer(New(target, consoleOrigin, refuseAll))
	t.Cleanup(proxy.Close)
	return proxy.URL
}

func TestOnlyTheConsoleAndTheServiceItselfMayFrameIt(t *testing.T) {
	proxy := serveThroughProxy(t, "/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Frame-Options", "SAMEORIGIN")
		w.Header().Add("Content-Security-Policy", "default-src 'self'; frame-ancestors 'none'; img-src data:")
		w.Header().Add("Content-Security-Policy", "frame-ancestors 'self'")
		io.WriteString(w, "hello")
	})

	resp, err := http.Get(proxy)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	if xfo := resp.Header.Get("X-Frame-Options"); xfo != "" {
		t.Errorf("X-Frame-Options = %q, want it removed", xfo)
	}
	got := resp.Header.Values("Content-Security-Policy")
	want := []string{
		"default-src 'self'; img-src data:",
		"frame-ancestors 'self' " + consoleOrigin,
	}
	if !slices.Equal(got, want) {
		t.Errorf("Content-Security-Policy = %q, want %q", got, want)
	}
}

func TestTheServiceGetsTheBrowsersHostAndPath(t *testing.T) {
	proxy := serveThroughProxy(t, "/start/", func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, r.Host+" "+r.URL.RequestURI())
	})

	resp, err := http.Get(proxy + "/other?x=1")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	// Services such as Grafana refuse requests whose Origin and Host disagree,
	// and the browser's Origin is the proxy's.
	want := strings.TrimPrefix(proxy, "http://") + " /other?x=1"
	if string(body) != want {
		t.Errorf("service saw %q, want %q", body, want)
	}
}

func TestRequestsAddressedToAnotherHostAreRefused(t *testing.T) {
	var reached atomic.Bool
	proxy := serveThroughProxy(t, "/", func(w http.ResponseWriter, r *http.Request) {
		reached.Store(true)
	})

	req, _ := http.NewRequest(http.MethodGet, proxy, nil)
	req.Host = "rebound.example"
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden || reached.Load() {
		t.Errorf("status %d, service reached %v; want 403 without reaching it", resp.StatusCode, reached.Load())
	}
}

func TestServerSentEventsArriveBeforeTheStreamEnds(t *testing.T) {
	done := make(chan struct{})
	defer close(done)
	proxy := serveThroughProxy(t, "/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "data: one\n\n")
		w.(http.Flusher).Flush()
		select {
		case <-done:
		case <-r.Context().Done():
		}
	})

	resp, err := http.Get(proxy)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	line := make(chan string, 1)
	go func() {
		l, _ := bufio.NewReader(resp.Body).ReadString('\n')
		line <- l
	}()
	select {
	case l := <-line:
		if l != "data: one\n" {
			t.Errorf("first line %q, want the event", l)
		}
	case <-time.After(2 * time.Second):
		t.Error("the event did not arrive while the stream was open")
	}
}

func TestUpgradedConnectionsPassThrough(t *testing.T) {
	proxy := serveThroughProxy(t, "/", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Upgrade") != "echo" {
			http.Error(w, "upgrade required", http.StatusUpgradeRequired)
			return
		}
		conn, buf, err := http.NewResponseController(w).Hijack()
		if err != nil {
			return
		}
		defer conn.Close()
		buf.WriteString("HTTP/1.1 101 Switching Protocols\r\nConnection: Upgrade\r\nUpgrade: echo\r\n\r\n")
		buf.Flush()
		io.Copy(conn, buf)
	})

	host := strings.TrimPrefix(proxy, "http://")
	conn, err := net.Dial("tcp", host)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(5 * time.Second))
	fmt.Fprintf(conn, "GET / HTTP/1.1\r\nHost: %s\r\nConnection: Upgrade\r\nUpgrade: echo\r\n\r\n", host)
	br := bufio.NewReader(conn)
	resp, err := http.ReadResponse(br, nil)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("status %d, want 101", resp.StatusCode)
	}
	io.WriteString(conn, "ping\n")
	if got, _ := br.ReadString('\n'); got != "ping\n" {
		t.Errorf("echo %q, want ping", got)
	}
}

func TestAnUnreachableServiceGetsAPageInTheBrowsersLanguage(t *testing.T) {
	closed := httptest.NewServer(http.NotFoundHandler())
	closed.Close()
	target, _ := url.Parse(closed.URL)
	proxy := httptest.NewServer(New(target, consoleOrigin, refuseAll))
	defer proxy.Close()

	for lang, want := range map[string]string{"zh-CN,zh;q=0.9": "连不上", "en-US,en;q=0.9": "Cannot reach"} {
		req, _ := http.NewRequest(http.MethodGet, proxy.URL, nil)
		req.Header.Set("Accept-Language", lang)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadGateway || !strings.Contains(string(body), want) || !strings.Contains(string(body), target.Host) {
			t.Errorf("%s: status %d, body %q; want 502 saying %s %s", lang, resp.StatusCode, body, want, target.Host)
		}
	}
}

func TestTargetsOutsideTheLocalNetworkAreRefusedWhenDialing(t *testing.T) {
	target, _ := url.Parse("http://93.184.215.14:80/")
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1/", nil)
	New(target, consoleOrigin, refuseAll).ServeHTTP(rec, req)
	body := rec.Body.String()
	if rec.Code != http.StatusBadGateway || !strings.Contains(body, "not on this machine or the local network") {
		t.Errorf("status %d, body %q; want 502 refusing non-local target", rec.Code, body)
	}
}

func TestClearingSiteDataTakesAFreshTokenAndNeverReachesTheService(t *testing.T) {
	var reached atomic.Bool
	used := false
	redeem := func(token string) bool {
		if token == "fresh" && !used {
			used = true
			return true
		}
		return false
	}
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { reached.Store(true) }))
	defer up.Close()
	target, _ := url.Parse(up.URL)
	proxy := httptest.NewServer(New(target, consoleOrigin, redeem))
	defer proxy.Close()

	resp, err := http.Get(proxy.URL + ClearPath + "?token=fresh")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Clear-Site-Data") != `"cache", "storage"` || resp.Header.Get("Cache-Control") != "no-store" {
		t.Errorf("with a fresh token: status %d, Clear-Site-Data %q, Cache-Control %q", resp.StatusCode, resp.Header.Get("Clear-Site-Data"), resp.Header.Get("Cache-Control"))
	}

	for _, query := range []string{"?token=fresh", "?token=guess", ""} {
		resp, err := http.Get(proxy.URL + ClearPath + query)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusForbidden || resp.Header.Get("Clear-Site-Data") != "" {
			t.Errorf("%q: status %d, Clear-Site-Data %q; want 403 and nothing cleared", query, resp.StatusCode, resp.Header.Get("Clear-Site-Data"))
		}
	}
	if reached.Load() {
		t.Error("the service saw a request for the clearing path")
	}
}
