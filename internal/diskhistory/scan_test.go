package diskhistory

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestAScanCountsFilesAtEveryDepthAndExcludesItsOwnDataAndLinks(t *testing.T) {
	root := t.TempDir()
	pluginDir := filepath.Join(root, "plugin")
	deep := filepath.Join(root, "目录", "two", "three", "four")
	if err := os.MkdirAll(deep, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(pluginDir, 0700); err != nil {
		t.Fatal(err)
	}
	for path, bytes := range map[string]string{filepath.Join(deep, "file"): "12345", filepath.Join(pluginDir, "history"): "ignore me"} {
		if err := os.WriteFile(path, []byte(bytes), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if runtime.GOOS != "windows" {
		if err := os.Symlink(root, filepath.Join(root, "cycle")); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(t.TempDir(), filepath.Join(root, "external")); err != nil {
			t.Fatal(err)
		}
	}
	got, err := collect(context.Background(), root, pluginDir)
	if err != nil {
		t.Fatal(err)
	}
	if got.Sizes["."].Bytes != 5 || got.Sizes["目录"].Bytes != 5 || got.Sizes[filepath.Join("目录", "two", "three")].Bytes != 5 || len(got.Sizes) != 4 {
		t.Fatalf("sizes %+v, want three directory levels and a root total excluding plugin files", got.Sizes)
	}
}

func TestAnUnreadableDirectoryCannotAppearAsADeletedDirectory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chmod does not remove Windows read access")
	}
	root, exclude := t.TempDir(), t.TempDir()
	denied := filepath.Join(root, "denied")
	if err := os.Mkdir(denied, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(denied, "file"), []byte("12345"), 0600); err != nil {
		t.Fatal(err)
	}
	before, err := collect(context.Background(), root, exclude)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(denied, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(denied, 0700) })
	if _, err := os.ReadDir(denied); err == nil {
		t.Skip("this user bypasses directory permissions")
	}
	after, err := collect(context.Background(), root, exclude)
	if err != nil {
		t.Fatal(err)
	}
	if !after.Sizes["."].Partial || !after.Sizes["denied"].Partial || after.Skipped != 1 {
		t.Fatalf("partial scan %+v", after)
	}
	if d, ok := difference(before, after, "denied"); ok {
		t.Fatalf("unreadable directory reported a change of %d", d)
	}
}

func TestACancelledScanDoesNotReturnAPublishableSnapshot(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s, err := collect(ctx, t.TempDir(), t.TempDir())
	if !errors.Is(err, context.Canceled) || s.Sizes != nil {
		t.Fatalf("snapshot %+v, error %v", s, err)
	}
}

func TestDirectorySummaryLimitsKeepTheFullRootTotal(t *testing.T) {
	root := t.TempDir()
	for i := range maxDirs + 5 {
		dir := filepath.Join(root, fmt.Sprintf("directory-%04d", i))
		if err := os.Mkdir(dir, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "file"), []byte("12345"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	s, err := collect(context.Background(), root, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Sizes) != maxDirs || !s.Truncated || s.Sizes["."].Bytes != int64((maxDirs+5)*5) || s.Sizes["."].Partial {
		t.Fatalf("limited snapshot %+v", s)
	}
}
