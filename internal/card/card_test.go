package card

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/Ike-li/hostdeck/internal/plugin"
)

func serve(t *testing.T, status int, body string) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func declarative(url, path string) plugin.Plugin {
	return plugin.Plugin{
		ID:   "demo",
		Name: plugin.Text{All: "Demo"},
		Icon: "D",
		Card: &plugin.Card{
			HTTP:  &plugin.HTTP{URL: url},
			Value: path,
			Label: plugin.Text{ByLang: map[string]string{"en": "Database", "zh-CN": "数据库"}},
		},
	}
}

func TestADeclarativeCardShowsTheValueAtItsPath(t *testing.T) {
	url := serve(t, 200, `{"db": {"state": "ok"}, "version": "1.2.3"}`)
	p := declarative(url, "db.state")

	got := Fetch(context.Background(), p)

	want := View{ID: "demo", Name: p.Name, Icon: "D", Value: "ok", Label: p.Card.Label}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestAFailedFetchBecomesAnErrorCardSayingWhy(t *testing.T) {
	closed := httptest.NewServer(http.NotFoundHandler())
	closed.Close()
	for _, tc := range []struct {
		name, url, path, mention string
	}{
		{"unreachable", closed.URL, "status", strings.TrimPrefix(closed.URL, "http://")},
		{"error status", serve(t, 500, `{"status": "ok"}`), "status", "500"},
		{"not JSON", serve(t, 200, `<html>`), "status", "not JSON"},
		{"nothing at the path", serve(t, 200, `{"state": "ok"}`), "status", `"status"`},
	} {
		got := Fetch(context.Background(), declarative(tc.url, tc.path))
		if got.Error == nil || got.Error.Code != "fetch_failed" || !strings.Contains(got.Error.Detail, tc.mention) {
			t.Errorf("%s: error = %+v, want fetch_failed mentioning %s", tc.name, got.Error, tc.mention)
		}
		if got.Value != "" {
			t.Errorf("%s: value = %q, want none", tc.name, got.Value)
		}
	}
}
