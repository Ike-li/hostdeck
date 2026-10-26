package console

import (
	"errors"
	"net/http"

	"github.com/Ike-li/hostdeck/internal/card"
	"github.com/Ike-li/hostdeck/internal/diskhistory"
	"github.com/Ike-li/hostdeck/internal/plugin"
)

func (s *server) serveDiskHistory(w http.ResponseWriter, r *http.Request) {
	p, ok := s.plugin(r.PathValue("id"))
	if !ok || !diskhistory.IsNative(p) {
		writeError(w, http.StatusNotFound, &card.Error{Code: "no_detail"})
		return
	}
	q := r.URL.Query()
	report, err := diskhistory.ReadReport(r.Context(), p.Dir, diskhistory.Query{Target: q.Get("target"), Path: q.Get("path"), Range: q.Get("range"), From: q.Get("from"), To: q.Get("to")})
	if errors.Is(err, diskhistory.ErrQuery) {
		writeError(w, http.StatusBadRequest, &card.Error{Code: "bad_request", Detail: err.Error()})
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, &card.Error{Code: "reported", Text: plugin.Text{ByLang: map[string]string{"en": "Disk history could not be read: " + err.Error(), "zh-CN": "无法读取磁盘历史：" + err.Error()}}})
		return
	}
	writeJSON(w, report)
}
