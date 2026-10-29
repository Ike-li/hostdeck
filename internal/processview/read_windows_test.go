package processview

import (
	"context"
	"github.com/Ike-li/hostdeck/internal/run"
	"testing"
)

func TestWindowsDoesNotInvokeAnExternalProcess(t *testing.T) {
	if testing.Short() {
		t.Skip("real process table")
	}
	if _, err := readPlatform(context.Background(), "", func(context.Context, run.Command) ([]byte, error) {
		t.Fatal("unexpected external process")
		return nil, nil
	}, true); err != nil {
		t.Fatal(err)
	}
}
