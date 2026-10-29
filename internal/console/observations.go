package console

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"

	"github.com/Ike-li/hostdeck/examples"
	"github.com/Ike-li/hostdeck/internal/card"
	"github.com/Ike-li/hostdeck/internal/detail"
	"github.com/Ike-li/hostdeck/internal/observation"
	"github.com/Ike-li/hostdeck/internal/plugin"
	"github.com/Ike-li/hostdeck/internal/run"
	"go.yaml.in/yaml/v3"
)

// Execution configurations, not display names or ids, grant this private
// capability. Jobs additionally require the shipped scripts: arbitrary detail
// output must never become authority to open a file on the console's behalf.
func observationKind(p plugin.Plugin) string {
	for _, kind := range []string{"jobs", "listeners", "manual"} {
		manifest, err := fs.ReadFile(examples.Files, kind+"/plugin.yaml")
		if err != nil {
			continue
		}
		var expected plugin.Plugin
		if yaml.Unmarshal(manifest, &expected) != nil {
			continue
		}
		if !reflect.DeepEqual(p.Detail, expected.Detail) || !reflect.DeepEqual(p.Actions, expected.Actions) {
			continue
		}
		if kind == "jobs" {
			entries, _ := fs.ReadDir(examples.Files, kind)
			matches := true
			for _, entry := range entries {
				if entry.Name() == "plugin.yaml" || entry.IsDir() {
					continue
				}
				want, _ := fs.ReadFile(examples.Files, kind+"/"+entry.Name())
				got, err := os.ReadFile(filepath.Join(p.Dir, entry.Name()))
				if err != nil || string(got) != string(want) {
					matches = false
					break
				}
			}
			if !matches {
				continue
			}
		}
		return kind
	}
	return ""
}
func (s *server) observationPlugin(id string) (plugin.Plugin, string, bool) {
	p, ok := s.plugin(id)
	kind := observationKind(p)
	return p, kind, ok && kind != ""
}
func (s *server) observationError(w http.ResponseWriter, status int, err error) {
	writeError(w, status, &card.Error{Code: "observation_failed", Detail: err.Error()})
}
func (s *server) serveObservationCreate(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Origin") != s.cfg.Origin {
		writeError(w, http.StatusForbidden, &card.Error{Code: "not_console"})
		return
	}
	p, kind, ok := s.observationPlugin(r.PathValue("id"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	var req struct {
		Item     string                `json:"item"`
		Action   string                `json:"action"`
		Expected *observation.Identity `json:"expected,omitempty"`
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		s.observationError(w, 400, err)
		return
	}
	target, err := s.resolveObservation(r.Context(), p, kind, req.Item, observation.Identity{})
	if err != nil {
		s.observationError(w, 502, err)
		return
	}
	if kind != "jobs" && (req.Expected == nil || !req.Expected.Equal(target.Identity)) {
		writeError(w, http.StatusConflict, &card.Error{Code: "observation_identity"})
		return
	}
	id, err := s.observations.Create(p.ID, req.Item, req.Action, target)
	if err != nil {
		s.observationError(w, 409, err)
		return
	}
	// Read-only openings already resolved and verified the source. Return the
	// first bounded reading now instead of resolving the same source again in
	// a second request. Action sessions still establish their positions at Begin.
	if req.Action == "" {
		snapshot, err := s.observations.Read(r.Context(), id, "", target)
		if err != nil {
			s.observationError(w, 500, err)
			return
		}
		writeJSON(w, struct {
			ID       string               `json:"id"`
			Snapshot observation.Snapshot `json:"snapshot"`
		}{id, snapshot})
		return
	}
	writeJSON(w, map[string]string{"id": id})
}
func (s *server) serveObservationRead(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Origin") != s.cfg.Origin {
		writeError(w, http.StatusForbidden, &card.Error{Code: "not_console"})
		return
	}
	var req struct {
		Cursor string `json:"cursor"`
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		s.observationError(w, 400, err)
		return
	}
	id := r.PathValue("session")
	pluginID, item, _, identity, err := s.observations.Binding(id)
	if err != nil {
		s.observationError(w, 404, err)
		return
	}
	p, kind, ok := s.observationPlugin(pluginID)
	if !ok {
		http.NotFound(w, r)
		return
	}
	target, captured, err := s.observations.SessionCaptureTarget(id)
	if err == nil && !captured {
		target, err = s.resolveObservation(r.Context(), p, kind, item, identity)
	}
	if err != nil {
		s.observationError(w, 502, err)
		return
	}
	snap, err := s.observations.Read(r.Context(), id, req.Cursor, target)
	if err != nil {
		s.observationError(w, 500, err)
		return
	}
	writeJSON(w, snap)
}
func (s *server) serveOperations(w http.ResponseWriter, r *http.Request) {
	_, _, ok := s.observationPlugin(r.PathValue("id"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	recent, err := s.observations.Recent(r.PathValue("id"))
	if err != nil {
		s.observationError(w, 500, err)
		return
	}
	writeJSON(w, recent)
}
func (s *server) resolveObservation(ctx context.Context, p plugin.Plugin, kind, item string, bound observation.Identity) (observation.Target, error) {
	listing := readObservationSources(ctx, p, kind, item, 0)
	if listing.Error != nil {
		return observation.Target{}, fmt.Errorf("%s: %s", listing.Error.Code, listing.Error.Detail)
	}
	for _, it := range listing.Items {
		if it.ID != item {
			continue
		}
		target := observation.Target{Name: it.Name, State: "unknown"}
		fields := map[string]string{}
		for _, f := range it.Fields {
			fields[textEnglish(f.Name)] = f.Value.String()
		}
		pid, _ := strconv.Atoi(fields["PID"])
		target.Identity = observation.ProcessIdentity(pid)
		if kind == "jobs" {
			switch strings.ToLower(fields["State"]) {
			case "running":
				target.State = "running"
			case "idle", "not running", "stopped", "disabled", "exited":
				target.State = "idle"
			}
			if strings.HasPrefix(strings.ToLower(fields["State"]), "not running") || it.Group == "idle" || it.Group == "disabled" {
				target.State = "idle"
			}
			if it.Group == "running" {
				target.State = "running"
			}
			if n, err := strconv.Atoi(fields["Last exit"]); err == nil {
				target.ExitCode = &n
			}
			target.Signal = fields["Last signal"]
			target.Runs = fields["Runs"]
			if runtime.GOOS == "windows" {
				target.Reason = "events"
				target.Text = it.Log
				if it.Log == "" {
					target.Reason = "events_empty"
				}
				if note := strings.ToLower(fields["Log reading"]); strings.Contains(note, "could not read") {
					target.Reason = "events_unreadable"
					if strings.Contains(note, "access is denied") || strings.Contains(note, "unauthorized") {
						target.Reason = "permission"
					}
				}
			} else {
				for _, key := range []string{"Standard output", "Standard error"} {
					if path := fields[key]; filepath.IsAbs(path) {
						target.Files = append(target.Files, path)
					}
				}
				if len(target.Files) == 0 {
					note := strings.ToLower(fields["Configuration"])
					if strings.Contains(note, "could not read") {
						target.Reason = "configuration_unreadable"
					} else if strings.Contains(note, "no configuration path") {
						target.Reason = "configuration_missing"
					}
				}
			}
			return target, nil
		}
		if !bound.Started.IsZero() && !bound.Equal(target.Identity) {
			return observation.Target{Identity: bound, State: "unknown", Reason: "identity_changed"}, nil
		}
		if target.Identity.PID > 1 {
			target.State = "running"
		}
		if target.Identity.PID <= 1 || target.Identity.Started.IsZero() {
			target.Reason = "identity_changed"
			return target, nil
		}
		if captured, ok, err := s.observations.FindCapture(target.Identity); err != nil {
			return target, err
		} else if ok {
			return captured, nil
		}
		// Associate only an exact PID and kernel start identity from a verified job.
		for _, jobPlugin := range s.loaded().Plugins {
			if observationKind(jobPlugin) != "jobs" {
				continue
			}
			jobs := readObservationSources(ctx, jobPlugin, "jobs", "", target.Identity.PID)
			if jobs.Error != nil {
				continue
			}
			for _, job := range jobs.Items {
				for _, field := range job.Fields {
					if textEnglish(field.Name) != "PID" || field.Value.String() != strconv.Itoa(target.Identity.PID) {
						continue
					}
					associated, err := s.resolveObservation(ctx, jobPlugin, "jobs", job.ID, observation.Identity{})
					if err == nil && associated.Identity.Equal(target.Identity) && (len(associated.Files) > 0 || associated.Text != "") {
						return associated, nil
					}
				}
			}
		}
		target.Files, target.Reason = observation.ProcessFiles(ctx, target.Identity)
		if target.Reason == "identity_changed" {
			target.State = "unknown"
		}
		return target, nil
	}
	return observation.Target{Identity: bound, State: "unknown", Reason: "gone"}, nil
}

func textEnglish(t plugin.Text) string {
	if t.All != "" {
		return t.All
	}
	return t.ByLang["en"]
}

// readObservationSources is private to verified built-ins. It omits unrelated
// presentation fields and log bodies. Windows jobs retain their event reader.
func readObservationSources(ctx context.Context, p plugin.Plugin, kind, item string, pid int) detail.View {
	if kind == "jobs" && runtime.GOOS != "darwin" {
		if item != "" {
			return detail.ReadItem(ctx, p, item)
		}
		return detail.Read(ctx, p)
	}
	if len(item) > 256 || strings.ContainsAny(item, "\r\n") {
		return detail.View{Error: &card.Error{Code: "bad_request", Detail: "item"}}
	}
	ctx, cancel := context.WithTimeout(ctx, detail.ReadTimeout)
	defer cancel()
	filter := ""
	if pid > 1 {
		filter = strconv.Itoa(pid)
	}
	var printed struct {
		Items        []detail.Item  `json:"items"`
		Presentation string         `json:"presentation"`
		Groups       []detail.Group `json:"groups"`
		Note         plugin.Text    `json:"note"`
		Error        plugin.Text    `json:"error"`
	}
	argv := []string{"@hostdeck", kind, "sources"}
	if kind == "jobs" {
		argv = []string{"sh", "observation.sh", item, filter}
	}
	if err := run.JSON(ctx, p.Dir, argv, &printed); err != nil {
		return detail.View{Error: card.CommandError(ctx, err)}
	}
	if !printed.Error.IsZero() {
		return detail.View{Error: &card.Error{Code: "reported", Text: printed.Error}}
	}
	return detail.View{Items: printed.Items}
}
