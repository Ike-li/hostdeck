package diskhistory

import (
	"fmt"
	"os"
)

// An OS lock is released on process exit; an interrupted scan cannot leave
// a stale owner. A second console may read but must not overwrite history.
func claim(dir string) (*os.File, error) {
	r, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	if err := r.MkdirAll("data", 0700); err != nil {
		return nil, err
	}
	f, err := r.OpenFile("data/owner.lock", os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err := lock(f); err != nil {
		f.Close()
		return nil, fmt.Errorf("another console owns this history: %w", err)
	}
	return f, nil
}
