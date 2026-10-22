// Package ports gives each embedded service its own forwarding port on
// 127.0.0.1 and remembers it.
package ports

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
	"strconv"

	"github.com/Ike-li/hostdeck/internal/atomicfile"
)

// Opened is a service's forwarding port. Err says why it could not be opened;
// Port is then the port the service keeps.
type Opened struct {
	Port     int
	Listener net.Listener
	Err      error
}

// Listen opens a forwarding port for each id. A service that has none in file
// gets the first free port from `from` up, and file records it. After that the
// service keeps its port: the port is part of its origin, and the browser keeps
// the service's storage under that origin. If something else holds the port,
// the service gets an error instead of another port.
func Listen(file string, ids []string, from int) (map[string]Opened, error) {
	kept := map[string]int{}
	data, err := os.ReadFile(file)
	switch {
	case err == nil:
		if err := json.Unmarshal(data, &kept); err != nil {
			return nil, fmt.Errorf("%s: %w", file, err)
		}
	case !errors.Is(err, fs.ErrNotExist):
		return nil, err
	}
	taken := map[int]bool{}
	for _, port := range kept {
		taken[port] = true
	}
	opened := make(map[string]Opened, len(ids))
	added := false
	for _, id := range ids {
		if port, ok := kept[id]; ok {
			ln, err := listen(port)
			if err != nil {
				ln, err = nil, fmt.Errorf("port %d, which %s keeps, is in use by another program", port, id)
			}
			opened[id] = Opened{Port: port, Listener: ln, Err: err}
			continue
		}
		port, ln, err := firstFree(from, taken)
		if err != nil {
			opened[id] = Opened{Err: err}
			continue
		}
		kept[id], taken[port], added = port, true, true
		opened[id] = Opened{Port: port, Listener: ln}
	}
	if added {
		if err := save(file, kept); err != nil {
			for _, o := range opened {
				if o.Listener != nil {
					o.Listener.Close()
				}
			}
			return nil, err
		}
	}
	return opened, nil
}

func firstFree(from int, taken map[int]bool) (int, net.Listener, error) {
	for port := from; port <= 65535; port++ {
		if taken[port] {
			continue
		}
		if ln, err := listen(port); err == nil {
			return port, ln, nil
		}
	}
	return 0, nil, fmt.Errorf("no free port from %d up", from)
}

func listen(port int) (net.Listener, error) {
	return net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(port))
}

func save(file string, kept map[string]int) error {
	data, err := json.MarshalIndent(kept, "", "  ")
	if err != nil {
		return err
	}
	return atomicfile.Write(file, data)
}
