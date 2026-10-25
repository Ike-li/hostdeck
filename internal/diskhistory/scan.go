package diskhistory

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// collect counts the disk space regular files occupy, a hard-linked file once
// where the platform says so. Links, special files, other volumes and plugin
// data are excluded. Only directory summaries to depth three are retained.
func collect(ctx context.Context, target, exclude string) (snapshot, error) {
	result := snapshot{Time: time.Now().UTC(), Sizes: map[string]measurement{".": {}}, Allocated: true}
	seen := map[fileKey]bool{}
	var missed []string
	// miss records an entry that could not be read. One that disappeared
	// during the scan is gone rather than missed.
	miss := func(rel string, err error) {
		if errors.Is(err, fs.ErrNotExist) {
			return
		}
		result.Skipped++
		missed = append(missed, rel)
		if m, ok := result.Sizes[rel]; ok {
			m.Partial = true
			result.Sizes[rel] = m
		}
		mark(&result, rel, 0, true)
	}
	base, err := os.Stat(target)
	if err != nil {
		return snapshot{}, err
	}
	if !base.IsDir() {
		return snapshot{}, errors.New("scan root is not a directory")
	}
	actual, err := filepath.EvalSymlinks(target)
	if err != nil {
		return snapshot{}, err
	}
	exclude, err = filepath.EvalSymlinks(exclude)
	if err != nil {
		return snapshot{}, err
	}
	within := func(path, parent string) bool {
		rel, err := filepath.Rel(parent, path)
		return err == nil && (rel == "." || filepath.IsLocal(rel))
	}
	if within(actual, exclude) {
		return snapshot{}, errors.New("scan root is inside the plugin folder")
	}
	root, err := os.OpenRoot(actual)
	if err != nil {
		return snapshot{}, err
	}
	defer root.Close()
	err = fs.WalkDir(root.FS(), ".", func(name string, entry fs.DirEntry, walkErr error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		path := filepath.Join(actual, filepath.FromSlash(name))
		if path != actual && within(path, exclude) {
			if entry != nil && entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(actual, path)
		if err != nil {
			return err
		}
		if walkErr != nil {
			if path == actual {
				return walkErr
			}
			miss(rel, walkErr)
			return nil
		}
		if entry.Type()&(os.ModeSymlink|os.ModeIrregular) != 0 {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			miss(rel, err)
			return nil
		}
		if path != actual && skipEntry(info, base) {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if info.IsDir() {
			if rel != "." && len(strings.Split(rel, string(filepath.Separator))) <= 3 {
				rememberDirectory(&result, rel)
			}
			return nil
		}
		if info.Mode().IsRegular() {
			if key, linked := hardLink(info); linked {
				if seen[key] {
					return nil
				}
				seen[key] = true
			}
			bytes, err := allocated(path, info)
			if err != nil {
				miss(rel, err)
				return nil
			}
			mark(&result, rel, bytes, false)
		}
		return nil
	})
	if err != nil {
		return snapshot{}, err
	}
	slices.Sort(missed)
	for name, m := range result.Sizes {
		if m.Partial {
			m.Missed = digest(missed, name)
			result.Sizes[name] = m
		}
	}
	result.Time = time.Now().UTC()
	return result, nil
}

// digest names the missed entries below dir, or nothing when dir itself was
// missed and so has no reading to compare.
func digest(missed []string, dir string) string {
	h := sha256.New()
	for _, path := range missed {
		if path == dir {
			return ""
		}
		if dir == "." || strings.HasPrefix(path, dir+string(filepath.Separator)) {
			io.WriteString(h, path)
			h.Write([]byte{0})
		}
	}
	return hex.EncodeToString(h.Sum(nil)[:8])
}

// Prefer shallow summaries when storage is full. Depth-first traversal must
// not let an early subtree evict every later top-level folder from the card.
func rememberDirectory(s *snapshot, path string) {
	if len(s.Sizes) < maxDirs {
		s.Sizes[path] = measurement{}
		return
	}
	s.Truncated = true
	depth := strings.Count(path, string(filepath.Separator)) + 1
	victim, deepest := "", depth
	for name := range s.Sizes {
		if name == "." {
			continue
		}
		d := strings.Count(name, string(filepath.Separator)) + 1
		if d > deepest || (d == deepest && d > depth && name > victim) {
			victim, deepest = name, d
		}
	}
	if victim != "" {
		delete(s.Sizes, victim)
		s.Sizes[path] = measurement{}
	}
}

func mark(s *snapshot, path string, bytes int64, partial bool) {
	for parent := filepath.Dir(path); ; parent = filepath.Dir(parent) {
		if m, ok := s.Sizes[parent]; ok {
			m.Bytes += bytes
			m.Partial = m.Partial || partial
			s.Sizes[parent] = m
		}
		if parent == "." {
			return
		}
	}
}
