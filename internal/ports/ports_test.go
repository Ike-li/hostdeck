package ports

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// freePort returns a port that nothing listens on right now.
func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

// occupy listens on port the way another program would.
func occupy(t *testing.T, port int) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(port))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
}

func open(t *testing.T, file string, ids []string, from int) map[string]Opened {
	t.Helper()
	opened, err := Listen(file, ids, from)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for _, o := range opened {
			if o.Listener != nil {
				o.Listener.Close()
			}
		}
	})
	return opened
}

func closeAll(opened map[string]Opened) {
	for _, o := range opened {
		if o.Listener != nil {
			o.Listener.Close()
		}
	}
}

func saved(t *testing.T, file string) map[string]int {
	t.Helper()
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	var ports map[string]int
	if err := json.Unmarshal(data, &ports); err != nil {
		t.Fatal(err)
	}
	return ports
}

func TestNewServicesGetFreePortsAndKeepThem(t *testing.T) {
	file := filepath.Join(t.TempDir(), "ports.json")
	from := freePort(t)

	first := open(t, file, []string{"a", "b"}, from)
	a, b := first["a"], first["b"]
	if a.Err != nil || b.Err != nil || a.Port < from || b.Port <= a.Port {
		t.Fatalf("got a=%+v b=%+v, want two ports from %d up, a's first", a, b, from)
	}
	if got := saved(t, file); got["a"] != a.Port || got["b"] != b.Port {
		t.Errorf("saved %v, want a=%d b=%d", got, a.Port, b.Port)
	}
	closeAll(first)

	again := open(t, file, []string{"a", "b"}, from)
	if again["a"].Port != a.Port || again["b"].Port != b.Port {
		t.Errorf("second start gave a=%d b=%d, want the same ports %d %d", again["a"].Port, again["b"].Port, a.Port, b.Port)
	}
}

func TestAPortInUseIsSkippedForANewService(t *testing.T) {
	file := filepath.Join(t.TempDir(), "ports.json")
	busy := freePort(t)
	occupy(t, busy)

	got := open(t, file, []string{"a"}, busy)["a"]
	if got.Err != nil || got.Port <= busy {
		t.Errorf("got %+v, want a port after the busy %d", got, busy)
	}
}

func TestPortsKeptForOtherServicesAreNotHandedOut(t *testing.T) {
	file := filepath.Join(t.TempDir(), "ports.json")
	kept := freePort(t)
	os.WriteFile(file, []byte(`{"removed": `+strconv.Itoa(kept)+`}`), 0o644)

	got := open(t, file, []string{"new"}, kept)["new"]
	if got.Err != nil || got.Listener == nil || got.Port == kept {
		t.Errorf("got %+v, want a port other than %d, which is kept for another service", got, kept)
	}
}

// A service's port is its origin, and its browser storage belongs to that
// origin, so it is never moved to another port.
func TestAKeptPortInUseIsAnErrorNotAMove(t *testing.T) {
	file := filepath.Join(t.TempDir(), "ports.json")
	first := open(t, file, []string{"a"}, freePort(t))
	port := first["a"].Port
	closeAll(first)
	occupy(t, port)

	got := open(t, file, []string{"a"}, port)["a"]
	if got.Err == nil || !strings.Contains(got.Err.Error(), strconv.Itoa(port)) || got.Listener != nil {
		t.Errorf("got %+v, want an error naming port %d and no listener", got, port)
	}
	if kept := saved(t, file)["a"]; kept != port {
		t.Errorf("saved port %d, want %d kept", kept, port)
	}
}
