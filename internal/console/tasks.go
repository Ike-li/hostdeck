package console

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/Ike-li/hostdeck/internal/card"
	"github.com/Ike-li/hostdeck/internal/tasks"
)

func (s *server) taskDir(r *http.Request) (string, bool) {
	p, ok := s.plugin(r.PathValue("id"))
	if !ok || !tasks.IsNative(p) {
		return "", false
	}
	return p.Dir, true
}

func (s *server) readTasks(w http.ResponseWriter, r *http.Request, fn func(dir string) (any, error)) {
	dir, ok := s.taskDir(r)
	if !ok {
		writeError(w, http.StatusNotFound, &card.Error{Code: "not_installed"})
		return
	}
	res, err := fn(dir)
	if err != nil {
		writeTaskError(w, err)
		return
	}
	writeJSON(w, res)
}

func (s *server) changeTasks(w http.ResponseWriter, r *http.Request, limit int64, dst any, fn func(dir string) (any, error)) {
	if r.Header.Get("Origin") != s.cfg.Origin {
		writeError(w, http.StatusForbidden, &card.Error{Code: "not_console"})
		return
	}
	if dst != nil {
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, limit)).Decode(dst); err != nil {
			writeError(w, http.StatusBadRequest, &card.Error{Code: "bad_request", Detail: err.Error()})
			return
		}
	}
	dir, ok := s.taskDir(r)
	if !ok {
		writeError(w, http.StatusNotFound, &card.Error{Code: "not_installed"})
		return
	}
	s.changing.Lock()
	defer s.changing.Unlock()
	res, err := fn(dir)
	if err != nil {
		writeTaskError(w, err)
		return
	}
	if res == nil {
		res = struct{}{}
	}
	writeJSON(w, res)
}

func (s *server) serveTaskList(w http.ResponseWriter, r *http.Request) {
	s.readTasks(w, r, func(dir string) (any, error) {
		list, err := tasks.ListTasks(dir)
		return map[string]any{"tasks": list}, err
	})
}

func (s *server) serveTaskRead(w http.ResponseWriter, r *http.Request) {
	s.readTasks(w, r, func(dir string) (any, error) {
		return tasks.ReadTask(dir, r.PathValue("task"))
	})
}

func (s *server) serveTaskCreate(w http.ResponseWriter, r *http.Request) {
	s.serveTaskSave(w, r, "")
}

func (s *server) serveTaskUpdate(w http.ResponseWriter, r *http.Request) {
	s.serveTaskSave(w, r, r.PathValue("task"))
}

func (s *server) serveTaskSave(w http.ResponseWriter, r *http.Request, id string) {
	var form tasks.TaskForm
	s.changeTasks(w, r, (1<<20)+4096, &form, func(dir string) (any, error) {
		if id == "" {
			created, err := tasks.CreateTask(dir, form)
			return map[string]string{"id": created}, err
		}
		return nil, tasks.UpdateTask(dir, id, form)
	})
}

func (s *server) serveTaskDelete(w http.ResponseWriter, r *http.Request) {
	s.changeTasks(w, r, 0, nil, func(dir string) (any, error) {
		return nil, tasks.DeleteTask(dir, r.PathValue("task"))
	})
}

func writeTaskError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, tasks.ErrScriptExists):
		writeError(w, http.StatusConflict, &card.Error{Code: "script_exists"})
	case errors.Is(err, tasks.ErrScriptMissing):
		writeError(w, http.StatusNotFound, &card.Error{Code: "script_missing"})
	case errors.Is(err, tasks.ErrTaskMissing):
		writeError(w, http.StatusNotFound, &card.Error{Code: "task_missing"})
	case errors.Is(err, tasks.ErrTaskForm), errors.Is(err, tasks.ErrScriptName), errors.Is(err, tasks.ErrScriptContent), errors.Is(err, tasks.ErrScriptRefused):
		writeError(w, http.StatusBadRequest, &card.Error{Code: "bad_request", Detail: err.Error()})
	default:
		writeError(w, http.StatusConflict, &card.Error{Code: "task_unavailable", Detail: err.Error()})
	}
}
