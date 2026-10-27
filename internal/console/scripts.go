package console

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/Ike-li/hostdeck/internal/card"
	"github.com/Ike-li/hostdeck/internal/tasks"
)

func (s *server) taskDir() (string, bool) {
	for _, p := range s.loaded().Plugins {
		if p.ID == "scheduled-tasks" && tasks.IsNative(p) {
			return p.Dir, true
		}
	}
	return "", false
}

func (s *server) serveScriptList(w http.ResponseWriter, r *http.Request) {
	dir, ok := s.taskDir()
	if !ok {
		writeError(w, http.StatusNotFound, &card.Error{Code: "not_installed"})
		return
	}
	files, err := tasks.ListScripts(dir)
	if err != nil {
		writeScriptError(w, err)
		return
	}
	writeJSON(w, map[string]any{"files": files})
}

func (s *server) serveScriptRead(w http.ResponseWriter, r *http.Request) {
	dir, ok := s.taskDir()
	if !ok {
		writeError(w, http.StatusNotFound, &card.Error{Code: "not_installed"})
		return
	}
	content, err := tasks.ReadScript(dir, r.PathValue("name"))
	if err != nil {
		writeScriptError(w, err)
		return
	}
	writeJSON(w, map[string]string{"name": r.PathValue("name"), "content": content})
}

func (s *server) serveScriptCreate(w http.ResponseWriter, r *http.Request) {
	s.serveScriptWrite(w, r, "")
}

func (s *server) serveScriptUpdate(w http.ResponseWriter, r *http.Request) {
	s.serveScriptWrite(w, r, r.PathValue("name"))
}

func (s *server) serveScriptWrite(w http.ResponseWriter, r *http.Request, name string) {
	if r.Header.Get("Origin") != s.cfg.Origin {
		writeError(w, http.StatusForbidden, &card.Error{Code: "not_console"})
		return
	}
	var req struct {
		Name    string `json:"name"`
		Content string `json:"content"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, (2<<20)+4096)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, &card.Error{Code: "bad_request", Detail: err.Error()})
		return
	}
	if name == "" {
		name = req.Name
	}
	dir, ok := s.taskDir()
	if !ok {
		writeError(w, http.StatusNotFound, &card.Error{Code: "not_installed"})
		return
	}
	s.changing.Lock()
	defer s.changing.Unlock()
	var err error
	if r.PathValue("name") == "" {
		err = tasks.CreateScript(dir, name, req.Content)
	} else {
		err = tasks.UpdateScript(dir, name, req.Content)
	}
	if err != nil {
		writeScriptError(w, err)
		return
	}
	writeJSON(w, struct{}{})
}

func (s *server) serveScriptDelete(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Origin") != s.cfg.Origin {
		writeError(w, http.StatusForbidden, &card.Error{Code: "not_console"})
		return
	}
	dir, ok := s.taskDir()
	if !ok {
		writeError(w, http.StatusNotFound, &card.Error{Code: "not_installed"})
		return
	}
	s.changing.Lock()
	defer s.changing.Unlock()
	if err := tasks.DeleteScript(dir, r.PathValue("name")); err != nil {
		writeScriptError(w, err)
		return
	}
	writeJSON(w, struct{}{})
}

func (s *server) serveScriptRun(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Origin") != s.cfg.Origin {
		writeError(w, http.StatusForbidden, &card.Error{Code: "not_console"})
		return
	}
	dir, ok := s.taskDir()
	if !ok {
		writeError(w, http.StatusNotFound, &card.Error{Code: "not_installed"})
		return
	}
	var command []string
	if r.ContentLength != 0 {
		var req struct {
			Command []string `json:"command"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, &card.Error{Code: "bad_request", Detail: err.Error()})
			return
		}
		command = req.Command
	}
	s.changing.Lock()
	defer s.changing.Unlock()
	id, err := tasks.RunScript(dir, r.PathValue("name"), command)
	if err != nil {
		writeScriptError(w, err)
		return
	}
	writeJSON(w, map[string]string{"id": id})
}

func writeScriptError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, tasks.ErrScriptExists):
		writeError(w, http.StatusConflict, &card.Error{Code: "script_exists"})
	case errors.Is(err, tasks.ErrScriptMissing):
		writeError(w, http.StatusNotFound, &card.Error{Code: "script_missing"})
	case errors.Is(err, tasks.ErrTaskForm), errors.Is(err, tasks.ErrScriptName), errors.Is(err, tasks.ErrScriptContent), errors.Is(err, tasks.ErrScriptRefused):
		writeError(w, http.StatusBadRequest, &card.Error{Code: "bad_request", Detail: err.Error()})
	default:
		writeError(w, http.StatusConflict, &card.Error{Code: "task_unavailable", Detail: err.Error()})
	}
}
