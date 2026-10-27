package console

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/Ike-li/hostdeck/internal/card"
	"github.com/Ike-li/hostdeck/internal/tasks"
)

func (s *server) serveTaskList(w http.ResponseWriter, r *http.Request) {
	dir, ok := s.taskDir()
	if !ok {
		writeError(w, http.StatusNotFound, &card.Error{Code: "not_installed"})
		return
	}
	list, err := tasks.ListTasks(dir)
	if err != nil {
		writeTaskError(w, err)
		return
	}
	writeJSON(w, map[string]any{"tasks": list})
}

func (s *server) serveTaskRead(w http.ResponseWriter, r *http.Request) {
	dir, ok := s.taskDir()
	if !ok {
		writeError(w, http.StatusNotFound, &card.Error{Code: "not_installed"})
		return
	}
	form, err := tasks.ReadTask(dir, r.PathValue("id"))
	if err != nil {
		writeTaskError(w, err)
		return
	}
	writeJSON(w, form)
}

func (s *server) serveTaskCreate(w http.ResponseWriter, r *http.Request) {
	s.serveTaskSave(w, r, "")
}

func (s *server) serveTaskUpdate(w http.ResponseWriter, r *http.Request) {
	s.serveTaskSave(w, r, r.PathValue("id"))
}

func (s *server) serveTaskSave(w http.ResponseWriter, r *http.Request, id string) {
	if r.Header.Get("Origin") != s.cfg.Origin {
		writeError(w, http.StatusForbidden, &card.Error{Code: "not_console"})
		return
	}
	var form tasks.TaskForm
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, (1<<20)+4096)).Decode(&form); err != nil {
		writeError(w, http.StatusBadRequest, &card.Error{Code: "bad_request", Detail: err.Error()})
		return
	}
	dir, ok := s.taskDir()
	if !ok {
		writeError(w, http.StatusNotFound, &card.Error{Code: "not_installed"})
		return
	}
	s.changing.Lock()
	defer s.changing.Unlock()
	if id == "" {
		created, err := tasks.CreateTask(dir, form)
		if err != nil {
			writeTaskError(w, err)
			return
		}
		writeJSON(w, map[string]string{"id": created})
		return
	}
	if err := tasks.UpdateTask(dir, id, form); err != nil {
		writeTaskError(w, err)
		return
	}
	writeJSON(w, struct{}{})
}

func (s *server) serveTaskDelete(w http.ResponseWriter, r *http.Request) {
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
	if err := tasks.DeleteTask(dir, r.PathValue("id")); err != nil {
		writeTaskError(w, err)
		return
	}
	writeJSON(w, struct{}{})
}

func writeTaskError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, tasks.ErrTaskForm):
		writeError(w, http.StatusBadRequest, &card.Error{Code: "bad_request", Detail: err.Error()})
	case errors.Is(err, tasks.ErrTaskMissing):
		writeError(w, http.StatusNotFound, &card.Error{Code: "task_missing"})
	default:
		writeError(w, http.StatusConflict, &card.Error{Code: "task_unavailable", Detail: err.Error()})
	}
}
