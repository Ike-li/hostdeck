package atomicfile

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAFailedWriteLeavesNoTempFileBehind(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "layout.json")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := Write(target, []byte("{}")); err == nil {
		t.Fatal("Write over a directory succeeded")
	}
	if _, err := os.Stat(target + ".tmp"); !os.IsNotExist(err) {
		t.Errorf("temp file left behind: stat err = %v", err)
	}
}
