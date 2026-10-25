package examples

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/Ike-li/hostdeck/internal/card"
	"github.com/Ike-li/hostdeck/internal/detail"
)

func withJobInfo(t *testing.T, info string) {
	t.Helper()
	withLaunchctl(t, fmt.Sprintf(`#!/bin/sh
case "$2" in
"gui/$(id -u)") printf '\tservices = {\n\t0 0 com.example.job\n}\n' ;;
*) cat <<'JOBS_INFO'
%s
JOBS_INFO
;;
esac
`, info))
}

func TestAJobThatDiedFromASignalNeedsAttentionUntilItRunsAgain(t *testing.T) {
	for _, state := range []string{"not running", "running", "spawn scheduled"} {
		t.Run(state, func(t *testing.T) {
			withJobInfo(t, "\tstate = "+state+"\n\tlast terminating signal = Segmentation fault: 11")
			p := load(t, "jobs")
			c := card.Fetch(context.Background(), p)
			v := detail.Read(context.Background(), p)
			if c.Error != nil || v.Error != nil || len(v.Items) != 1 {
				t.Fatalf("card %+v, detail %+v", c, v)
			}
			it := v.Items[0]
			if state == "running" {
				if c.Status != "ok" || c.Value != "0" || it.Status != "ok" || it.Value != "running" {
					t.Errorf("a running job must ignore its previous signal: card %+v, item %+v", c, it)
				}
			} else if c.Status != "error" || c.Value != "1" || len(c.Rows) != 1 || it.Status != "error" || !strings.Contains(string(it.Value), "signal Segmentation fault: 11") {
				t.Errorf("card %+v, item %+v; want the signal death reported", c, it)
			}
			if !slices.ContainsFunc(it.Fields, func(f detail.Field) bool {
				return f.Name.ByLang["en"] == "Last signal" && f.Name.ByLang["zh-CN"] == "上次终止信号" && f.Value == "Segmentation fault: 11"
			}) {
				t.Errorf("fields %+v, want the last signal preserved", it.Fields)
			}
		})
	}
}

func TestAJobThatHasNeverRunIsIdleInsteadOfFailed(t *testing.T) {
	withJobInfo(t, "\tstate = not running\n\tlast exit code = (never exited)\n\truns = 0")
	p := load(t, "jobs")
	c := card.Fetch(context.Background(), p)
	v := detail.Read(context.Background(), p)
	if c.Error != nil || c.Status != "ok" || c.Value != "0" || len(c.Rows) != 0 {
		t.Errorf("card %+v, want a healthy zero for a job that never ran", c)
	}
	if v.Error != nil || len(v.Items) != 1 {
		t.Fatalf("detail %+v", v)
	}
	if it := v.Items[0]; it.Status != "ok" || it.Value != "idle" || len(it.Actions) != 1 || it.Actions[0].ID != "start" {
		t.Errorf("item %+v, want an idle job that may be run", it)
	}
}
