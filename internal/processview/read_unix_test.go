//go:build unix

package processview

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/Ike-li/hostdeck/internal/detail"
	"github.com/Ike-li/hostdeck/internal/run"
)

func TestTheUnixReaderPreservesSpacedNamesAndReportsCPUAndResidentMemory(t *testing.T) {
	out, err := Execute(context.Background(), t.TempDir(), []string{"processes", "detail"}, func(_ context.Context, c run.Command) ([]byte, error) {
		if !reflect.DeepEqual(c.Args, []string{"/bin/ps", "-axo", "pid=,ppid=,user=,%cpu=,rss=,comm="}) {
			t.Fatalf("command %v", c.Args)
		}
		found := false
		for _, e := range c.Env {
			if e == "LC_ALL=C" {
				found = true
			}
		}
		if !found {
			t.Fatal("numeric output locale is not pinned")
		}
		return []byte(" 42 1 example 125.5 32768 /Applications/My  Worker.app/Contents/MacOS/My  Worker\n 43 42 root 0.0 0 /usr/bin/other\n"), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	var view detail.View
	if err := run.Decode(out, &view); err != nil {
		t.Fatal(err)
	}
	if view.Error != nil || len(view.Items) != 2 || view.Items[0].Name.All != "My  Worker" {
		t.Fatalf("reply %s", out)
	}
	if view.Items[0].Fields[3].Value.String() != "125.5%" || view.Items[0].Fields[4].Value.String() != "32.0 MiB" {
		t.Fatalf("fields %+v", view.Items[0].Fields)
	}
}

func TestAnInvalidOrFailedUnixProcessTableNeverBecomesAPartialList(t *testing.T) {
	for _, body := range []string{"", "42 1 user 0.0 20 /bin/good\nbroken\n", "42 1 user NaN 20 /bin/bad\n", "42 1 user -1 20 /bin/bad\n", "42 1 user 0.0 -1 /bin/bad\n", "42 1 user 0.0 18446744073709551615 /bin/bad\n", "42 1 user 0.0 20 /bin/good\n42 1 user 0.0 20 /bin/duplicate\n"} {
		out, err := Execute(context.Background(), "", []string{"processes", "card"}, func(context.Context, run.Command) ([]byte, error) { return []byte(body), nil })
		if err != nil {
			t.Fatal(err)
		}
		var got struct {
			Error map[string]string
			Value string
		}
		if err := json.Unmarshal(out, &got); err != nil {
			t.Fatal(err)
		}
		if got.Error["en"] == "" || got.Value != "" {
			t.Fatalf("accepted %q: %s", body, out)
		}
	}
	out, err := Execute(context.Background(), "", []string{"processes", "card"}, func(context.Context, run.Command) ([]byte, error) {
		return []byte("42 1 user 0.0 20 /bin/good\n"), &run.ExitError{Code: 1}
	})
	if err != nil {
		t.Fatal(err)
	}
	var got struct{ Error map[string]string }
	json.Unmarshal(out, &got)
	if got.Error["en"] == "" {
		t.Fatalf("accepted failed command: %s", out)
	}
}
