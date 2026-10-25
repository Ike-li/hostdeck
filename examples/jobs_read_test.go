package examples

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Ike-li/hostdeck/internal/card"
	"github.com/Ike-li/hostdeck/internal/detail"
)

func TestAFailedJobsListReportsAnErrorInsteadOfAnEmptyHealthyReading(t *testing.T) {
	for _, output := range []string{"exit 1", "echo malformed"} {
		t.Run(output, func(t *testing.T) {
			seen := filepath.Join(t.TempDir(), "probed")
			withLaunchctl(t, fmt.Sprintf(`#!/bin/sh
if [ ! -f %q ]; then touch %q; exit 0; fi
%s
`, seen, seen, output))
			p := load(t, "jobs")
			c := card.Fetch(context.Background(), p)
			// Each read needs its own successful domain probe.
			withLaunchctl(t, fmt.Sprintf(`#!/bin/sh
if [ ! -f %q ]; then touch %q; exit 0; fi
%s
`, seen+"2", seen+"2", output))
			v := detail.Read(context.Background(), p)
			for _, err := range []*card.Error{c.Error, v.Error} {
				if err == nil || err.Code != "reported" || err.Text.ByLang["en"] != "Could not list launchd jobs for this user." || err.Text.ByLang["zh-CN"] != "读不到当前用户的 launchd 任务。" {
					t.Errorf("error %+v, want a bilingual list failure", err)
				}
			}
			if len(v.Items) != 0 || c.Value != "" {
				t.Fatalf("card %+v, detail %+v; a failed read must not invent data", c, v)
			}
		})
	}
}

func TestAnUnreadableJobReportsWhyAndCannotOfferAnAction(t *testing.T) {
	withLaunchctl(t, `#!/bin/sh
case "$2" in
"gui/$(id -u)") printf '\tservices = {\n\t0 0 com.example.vanished\n}\n' ;;
*) exit 1 ;;
esac
`)
	p := load(t, "jobs")
	c := card.Fetch(context.Background(), p)
	v := detail.Read(context.Background(), p)
	for _, err := range []*card.Error{c.Error, v.Error} {
		if err == nil || err.Code != "reported" || !strings.Contains(err.Text.ByLang["en"], "com.example.vanished") || !strings.Contains(err.Text.ByLang["zh-CN"], "com.example.vanished") {
			t.Errorf("error %+v, want the unreadable job named in both languages", err)
		}
	}
	if len(v.Items) != 0 {
		t.Errorf("items %+v, want no actions based on an unreadable job", v.Items)
	}
}
