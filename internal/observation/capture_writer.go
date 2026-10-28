package observation

import (
	"io"
	"os"
	"path/filepath"
	"sync"
)

const segmentLimit = 2 << 20

type captureWriter struct {
	mu      sync.Mutex
	dir     string
	file    *os.File
	size    int
	failure error
}

func newCaptureWriter(dir string) (*captureWriter, error) {
	f, err := os.OpenFile(filepath.Join(dir, "output.log"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return nil, err
	}
	return &captureWriter{dir: dir, file: f}, nil
}
func (w *captureWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	n := len(p)
	for len(p) > 0 && w.failure == nil {
		if w.size == segmentLimit {
			if err := w.file.Close(); err != nil {
				w.failure = err
				break
			}
			if err := os.Remove(filepath.Join(w.dir, "previous.log")); err != nil && !os.IsNotExist(err) {
				w.failure = err
				break
			}
			if err := os.Rename(filepath.Join(w.dir, "output.log"), filepath.Join(w.dir, "previous.log")); err != nil {
				w.failure = err
				break
			}
			w.file, w.failure = os.OpenFile(filepath.Join(w.dir, "output.log"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
			w.size = 0
			if w.failure != nil {
				break
			}
		}
		count := min(len(p), segmentLimit-w.size)
		written, err := w.file.Write(p[:count])
		w.size += written
		p = p[written:]
		if err != nil {
			w.failure = err
		} else if written < count {
			w.failure = io.ErrShortWrite
		}
	}
	// Keep draining a failed capture so storage failure cannot block the service.
	return n, nil
}
func (w *captureWriter) Err() error { w.mu.Lock(); defer w.mu.Unlock(); return w.failure }
func (w *captureWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file == nil {
		return w.failure
	}
	err := w.file.Close()
	w.file = nil
	if w.failure != nil {
		return w.failure
	}
	return err
}
