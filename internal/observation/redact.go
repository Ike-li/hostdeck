package observation

import (
	"io"
	"slices"
	"strings"
	"sync"
)

// Only credential values, never the launch environment itself, are retained in
// this in-memory streaming filter. The pending suffix handles split writes.
type credentialWriter struct {
	mu          sync.Mutex
	destination io.Writer
	secrets     []string
	pending     string
	longest     int
}

func newCredentialWriter(destination io.Writer, environment []string) *credentialWriter {
	out := &credentialWriter{destination: destination, longest: 1}
	for _, entry := range environment {
		key, value, ok := strings.Cut(entry, "=")
		if !ok || value == "" {
			continue
		}
		upper := strings.ToUpper(key)
		secret := false
		for _, word := range []string{"TOKEN", "SECRET", "PASSWORD", "PASSWD", "API_KEY", "APIKEY", "PRIVATE_KEY", "ACCESS_KEY", "CREDENTIAL", "AUTHORIZATION", "COOKIE"} {
			if strings.Contains(upper, word) {
				secret = true
				break
			}
		}
		if secret && !slices.Contains(out.secrets, value) {
			out.secrets = append(out.secrets, value)
			out.longest = max(out.longest, len(value))
		}
	}
	slices.SortFunc(out.secrets, func(a, b string) int { return len(b) - len(a) })
	return out
}
func (w *credentialWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.pending += string(p)
	w.flush(false)
	return len(p), nil
}
func (w *credentialWriter) flush(final bool) {
	for len(w.pending) > 0 {
		position, length := -1, 0
		for _, secret := range w.secrets {
			at := strings.Index(w.pending, secret)
			if at >= 0 && (position < 0 || at < position) {
				position, length = at, len(secret)
			}
		}
		if position >= 0 {
			io.WriteString(w.destination, w.pending[:position])
			io.WriteString(w.destination, "***")
			w.pending = w.pending[position+length:]
			continue
		}
		keep := 0
		if !final {
			for _, secret := range w.secrets {
				start := max(0, len(w.pending)-len(secret)+1)
				for start < len(w.pending) {
					index := strings.IndexByte(w.pending[start:], secret[0])
					if index < 0 {
						break
					}
					start += index
					suffix := w.pending[start:]
					if strings.HasPrefix(secret, suffix) {
						keep = max(keep, len(suffix))
						break
					}
					start++
				}
			}
		}
		safe := len(w.pending) - keep
		if safe <= 0 {
			return
		}
		io.WriteString(w.destination, w.pending[:safe])
		w.pending = w.pending[safe:]
		if keep > 0 {
			return
		}
	}
}
func (w *credentialWriter) Flush() { w.mu.Lock(); defer w.mu.Unlock(); w.flush(true) }
