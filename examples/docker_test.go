package examples

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Ike-li/hostdeck/internal/card"
	"github.com/Ike-li/hostdeck/internal/catalog"
	"github.com/Ike-li/hostdeck/internal/detail"
	"github.com/Ike-li/hostdeck/internal/plugin"
)

func TestDockerInstallsAsAnIndependentNativePluginAndRefusesRemoteTargets(t *testing.T) {
	t.Setenv("DOCKER_CONTEXT", "")
	t.Setenv("DOCKER_HOST", "ssh://remote.example")
	dir, err := catalog.Add(Files, t.TempDir(), "docker")
	if err != nil {
		t.Fatal(err)
	}
	p, err := plugin.ReadFolder(dir)
	if err != nil {
		t.Fatal(err)
	}
	if p.Card == nil || p.Detail == nil || p.Page != nil {
		t.Fatalf("not an independent card/detail plugin: %+v", p)
	}
	c := card.Fetch(context.Background(), p)
	v := detail.Read(context.Background(), p)
	for _, e := range []*card.Error{c.Error, v.Error} {
		if e == nil || e.Code != "reported" || e.Text.ByLang["en"] == "" || e.Text.ByLang["zh-CN"] == "" {
			t.Fatalf("missing bilingual refusal: %+v", e)
		}
	}
	files, err := os.ReadDir(dir)
	if err != nil || len(files) != 1 {
		t.Fatalf("native install files %v %v", files, err)
	}
}

func TestTheRealDockerCLIFormatsOnlyTheSelectedContainerInformation(t *testing.T) {
	if testing.Short() {
		t.Skip("real Docker CLI against a fake engine")
	}
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("Docker CLI is not installed")
	}
	id := strings.Repeat("a", 64)
	var healthEnabled atomic.Bool
	healthEnabled.Store(true)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("API-Version", "1.44")
		w.Header().Set("Content-Type", "application/json")
		path := r.URL.Path
		if path == "/_ping" {
			fmt.Fprint(w, "OK")
			return
		}
		if strings.HasSuffix(path, "/containers/json") {
			if r.URL.Query().Get("all") != "1" {
				t.Error("did not request stopped containers")
			}
			json.NewEncoder(w).Encode([]any{map[string]any{"Id": id, "Names": []string{"/web"}, "Image": "example/image:1", "State": "running", "Status": "Up"}})
			return
		}
		if strings.HasSuffix(path, "/containers/"+id+"/json") {
			payload := map[string]any{"Id": id, "Name": "/web", "Created": "2026-10-26T00:00:00Z", "Path": "/bin/app", "Args": []string{"--label", "a b"}, "RestartCount": 2, "State": map[string]any{"Status": "running", "Running": true, "ExitCode": 0, "OOMKilled": false, "Error": "", "StartedAt": "2026-10-26T00:00:00Z", "FinishedAt": "0001-01-01T00:00:00Z", "Health": map[string]any{"Status": "healthy", "Log": []any{map[string]any{"Output": "unrelated-health-output"}}}}, "Config": map[string]any{"Image": "example/image:1", "Tty": false, "Env": []string{"UNRELATED_SECRET=must-not-be-shown"}}, "HostConfig": map[string]any{"RestartPolicy": map[string]any{"Name": "unless-stopped"}}, "NetworkSettings": map[string]any{"Ports": map[string]any{"80/tcp": []map[string]string{{"HostIp": "127.0.0.1", "HostPort": "8080"}}}, "Networks": map[string]any{"app-net": map[string]any{"IPAddress": "172.18.0.2"}}}, "Mounts": []any{}}
			if !healthEnabled.Load() {
				delete(payload["State"].(map[string]any), "Health")
			}
			json.NewEncoder(w).Encode(payload)
			return
		}
		if strings.HasSuffix(path, "/containers/"+id+"/logs") {
			if r.URL.Query().Get("tail") != "20" || r.URL.Query().Get("follow") == "1" {
				t.Errorf("unbounded log query %s", r.URL.RawQuery)
			}
			w.Header().Set("Content-Type", "application/vnd.docker.raw-stream")
			for stream, line := range map[byte]string{1: "stdout real CLI\n", 2: "stderr real CLI 中文\n"} {
				header := make([]byte, 8)
				header[0] = stream
				binary.BigEndian.PutUint32(header[4:], uint32(len(line)))
				w.Write(header)
				fmt.Fprint(w, line)
			}
			return
		}
		t.Errorf("unexpected engine request %s %s", r.Method, r.URL.String())
		http.Error(w, "unexpected fixture request", 404)
	}))
	defer server.Close()
	t.Setenv("DOCKER_CONTEXT", "")
	t.Setenv("DOCKER_HOST", "tcp://"+strings.TrimPrefix(server.URL, "http://"))
	t.Setenv("DOCKER_TLS_VERIFY", "")
	t.Setenv("DOCKER_CERT_PATH", "")
	t.Setenv("DOCKER_API_VERSION", "1.44")
	dir, err := catalog.Add(Files, t.TempDir(), "docker")
	if err != nil {
		t.Fatal(err)
	}
	p, err := plugin.ReadFolder(dir)
	if err != nil {
		t.Fatal(err)
	}
	got := detail.Read(context.Background(), p)
	if got.Error != nil || len(got.Items) != 1 {
		t.Fatalf("native reading %+v error %+v", got, got.Error)
	}
	raw, _ := json.Marshal(got)
	for _, want := range []string{"example/image:1", "127.0.0.1:8080", "app-net", "stdout real CLI", "stderr real CLI 中文"} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("missing %q in %s", want, raw)
		}
	}
	for _, unrelated := range []string{"must-not-be-shown", "unrelated-health-output"} {
		if strings.Contains(string(raw), unrelated) {
			t.Fatalf("read an unrelated inspect field: %s", raw)
		}
	}
	healthEnabled.Store(false)
	got = detail.Read(context.Background(), p)
	if got.Error != nil || len(got.Items) != 1 || got.Items[0].Group != "running" {
		t.Fatalf("container without health check: %+v %+v", got, got.Error)
	}

}
