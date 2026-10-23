package cli

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/Ike-li/hostdeck/internal/plugin"
)

// installLocal is deliberately a filesystem operation in the CLI process.
// The HTTP API cannot submit commands, files or a source path for installation.
func installLocal(root, id, from string) (string, *Failure) {
	p, err := plugin.ReadFolder(from)
	if err != nil {
		return "", &Failure{"invalid_manifest", err.Error()}
	}
	source, err := filepath.EvalSymlinks(p.Dir)
	if err != nil {
		return "", &Failure{"invalid_source", err.Error()}
	}
	destination, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", &Failure{"install_failed", err.Error()}
	}
	rel, err := filepath.Rel(source, destination)
	if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", &Failure{"invalid_source", "source must not contain the destination plugin directory"}
	}
	// A regular lock file is ignored by both the loader and the management list.
	lockPath := filepath.Join(root, ".cli-install-"+id+".lock")
	lock, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		if errors.Is(err, fs.ErrExist) {
			return "", &Failure{"install_busy", "another install owns this ID; do not remove its lock while it is running"}
		}
		return "", &Failure{"install_failed", err.Error()}
	}
	lock.Close()
	defer os.Remove(lockPath)
	available := func() *Failure {
		for _, name := range []string{id, "." + id} {
			_, err := os.Lstat(filepath.Join(root, name))
			if err == nil {
				return &Failure{"already_added", "an enabled or disabled plugin already owns this ID"}
			}
			if !errors.Is(err, fs.ErrNotExist) {
				return &Failure{"install_failed", err.Error()}
			}
		}
		return nil
	}
	if problem := available(); problem != nil {
		return "", problem
	}
	opened, err := os.OpenRoot(source)
	if err != nil {
		return "", &Failure{"invalid_source", err.Error()}
	}
	defer opened.Close()
	sourceFS := opened.FS()
	err = fs.WalkDir(sourceFS, ".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 || (!entry.IsDir() && !entry.Type().IsRegular()) {
			return errors.New("plugin sources may contain only directories and regular files: " + path)
		}
		return nil
	})
	if err != nil {
		return "", &Failure{"invalid_source", err.Error()}
	}
	staging, err := os.MkdirTemp(root, ".cli-adding-")
	if err != nil {
		return "", &Failure{"install_failed", err.Error()}
	}
	defer os.RemoveAll(staging)
	if err = os.CopyFS(staging, sourceFS); err != nil {
		return "", &Failure{"install_failed", err.Error()}
	}
	// Check the copy that will actually be published, in case the source changed.
	if _, err = plugin.ReadFolder(staging); err != nil {
		return "", &Failure{"invalid_manifest", err.Error()}
	}
	if problem := available(); problem != nil {
		return "", problem
	}
	target := filepath.Join(root, id)
	if err = os.Rename(staging, target); err != nil {
		return "", &Failure{"install_failed", err.Error()}
	}
	return target, nil
}
