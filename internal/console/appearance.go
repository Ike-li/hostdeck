package console

import (
	"encoding/json"
	"html/template"
	"net/http"

	"github.com/Ike-li/hostdeck/internal/appearance"
	"github.com/Ike-li/hostdeck/internal/card"
	"github.com/Ike-li/hostdeck/web"
)

var indexPage = template.Must(template.New("index.html").ParseFS(web.Files, "index.html"))

type appearanceView struct {
	Appearance appearance.Settings `json:"appearance"`
	Warning    *card.Error         `json:"warning,omitempty"`
}

func (s *server) appearance() appearanceView {
	value, warning := s.cfg.Appearance.Get()
	view := appearanceView{Appearance: value}
	if warning != "" {
		view.Warning = &card.Error{Code: "appearance_invalid", Detail: warning}
	}
	return view
}

// The document carries the server's reading so the first content paint does
// not have to wait for an API call or trust a browser-local preference.
func (s *server) serveIndex(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	data, _ := json.Marshal(s.appearance())
	_ = indexPage.Execute(w, struct{ Appearance string }{string(data)})
}

func (s *server) serveAppearance(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, s.appearance())
}

func (s *server) serveAppearanceSave(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Origin") != s.cfg.Origin {
		writeError(w, http.StatusForbidden, &card.Error{Code: "not_console"})
		return
	}
	value, err := appearance.Decode(http.MaxBytesReader(w, r.Body, 4096))
	if err != nil {
		writeError(w, http.StatusBadRequest, &card.Error{Code: "bad_request", Detail: err.Error()})
		return
	}
	if err := s.cfg.Appearance.Set(value); err != nil {
		writeError(w, http.StatusInternalServerError, &card.Error{Code: "appearance_failed", Detail: err.Error()})
		return
	}
	s.serveAppearance(w, r)
}
