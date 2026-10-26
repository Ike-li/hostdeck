package tasks

import (
	"errors"
	"os"
	"sort"
	"strings"
)

type logWriter struct {
	file      *os.File
	secrets   []string
	pending   string
	size      int
	Truncated bool
	Err       error
}

func newLog(f *os.File, secrets []string) *logWriter {
	values := append([]string{}, secrets...)
	sort.Slice(values, func(i, j int) bool { return len(values[i]) > len(values[j]) })
	return &logWriter{file: f, secrets: values}
}
func (w *logWriter) Write(p []byte) (int, error) {
	w.pending += string(p)
	w.flush(false)
	return len(p), w.Err
}
func (w *logWriter) flush(final bool) {
	hold := 0
	for _, s := range w.secrets {
		hold = max(hold, len(s)-1)
	}
	if final {
		hold = 0
	}
	for len(w.pending) > hold {
		limit := len(w.pending) - hold
		at := -1
		secret := ""
		for _, s := range w.secrets {
			if s == "" {
				continue
			}
			i := strings.Index(w.pending, s)
			if i >= 0 && i < limit && (at < 0 || i < at) {
				at = i
				secret = s
			}
		}
		if at < 0 {
			w.write(w.pending[:limit])
			w.pending = w.pending[limit:]
			break
		}
		w.write(w.pending[:at])
		w.write("***")
		w.pending = w.pending[at+len(secret):]
	}
}
func (w *logWriter) write(s string) {
	room := (1 << 20) - w.size
	if len(s) > room {
		w.Truncated = true
		s = s[:room]
	}
	if s == "" || w.Err != nil {
		return
	}
	n, err := w.file.WriteString(s)
	w.size += n
	if err != nil {
		w.Err = &logIOError{err}
	}
}
func (w *logWriter) Close() error {
	w.flush(true)
	err := w.Err
	if err == nil {
		err = w.file.Sync()
	}
	return errors.Join(err, w.file.Close())
}

type logIOError struct{ error }

func redact(s string, secrets []string) string {
	for _, secret := range secrets {
		if secret != "" {
			s = strings.ReplaceAll(s, secret, "***")
		}
	}
	return s
}
