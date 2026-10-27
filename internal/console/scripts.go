package console

import (
	"net/http"

	"github.com/Ike-li/hostdeck/internal/tasks"
)

func (s *server) serveScriptList(w http.ResponseWriter, r *http.Request) {
	s.readTasks(w, r, func(dir string) (any, error) {
		files, err := tasks.ListScripts(dir)
		return map[string]any{"files": files}, err
	})
}

func (s *server) serveScriptRead(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	s.readTasks(w, r, func(dir string) (any, error) {
		content, err := tasks.ReadScript(dir, name)
		return map[string]string{"name": name, "content": content}, err
	})
}

func (s *server) serveScriptCreate(w http.ResponseWriter, r *http.Request) {
	s.serveScriptWrite(w, r, "")
}

func (s *server) serveScriptUpdate(w http.ResponseWriter, r *http.Request) {
	s.serveScriptWrite(w, r, r.PathValue("name"))
}

func (s *server) serveScriptWrite(w http.ResponseWriter, r *http.Request, name string) {
	var req struct {
		Name    string `json:"name"`
		Content string `json:"content"`
	}
	s.changeTasks(w, r, (2<<20)+4096, &req, func(dir string) (any, error) {
		if name == "" {
			return nil, tasks.CreateScript(dir, req.Name, req.Content)
		}
		return nil, tasks.UpdateScript(dir, name, req.Content)
	})
}

func (s *server) serveScriptDelete(w http.ResponseWriter, r *http.Request) {
	s.changeTasks(w, r, 0, nil, func(dir string) (any, error) {
		return nil, tasks.DeleteScript(dir, r.PathValue("name"))
	})
}

func (s *server) serveScriptRun(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Command []string `json:"command"`
	}
	var dst any
	if r.ContentLength != 0 {
		dst = &req
	}
	s.changeTasks(w, r, 1<<20, dst, func(dir string) (any, error) {
		id, err := tasks.RunScript(dir, r.PathValue("name"), req.Command)
		return map[string]string{"id": id}, err
	})
}
