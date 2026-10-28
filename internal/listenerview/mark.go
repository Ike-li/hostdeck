package listenerview

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

// loadForwarded reads ports.json beside the plugins folder. Missing file is an
// empty map; a corrupt file is ignored so a reading still lists listeners.
func loadForwarded(pluginDir string) (map[int]string, error) {
	root := filepath.Clean(filepath.Join(pluginDir, "..", ".."))
	data, err := os.ReadFile(filepath.Join(root, "ports.json"))
	switch {
	case err == nil:
	case errors.Is(err, fs.ErrNotExist):
		return map[int]string{}, nil
	default:
		return nil, err
	}
	kept := map[string]int{}
	if err := json.Unmarshal(data, &kept); err != nil {
		return map[int]string{}, nil
	}
	out := map[int]string{}
	for id, port := range kept {
		if port > 0 {
			out[port] = id
		}
	}
	return out, nil
}
