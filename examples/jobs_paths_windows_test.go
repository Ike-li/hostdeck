package examples

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/Ike-li/hostdeck/internal/detail"
	"github.com/Ike-li/hostdeck/internal/run"
)

func TestWindowsJobsResolveProgramPathsBeforeDecidingWhichServicesToList(t *testing.T) {
	root := os.Getenv("SystemRoot")
	volume := filepath.VolumeName(root)
	t.Setenv("HOSTDECK_JOBS_PROGRAM_ROOT", `C:\Program Files\Vendor`)
	t.Setenv("HOSTDECK_JOBS_MISSING_ROOT", "")
	cases := []struct{ id, command, program string }{
		{"quoted", `"C:\Program Files\Vendor\service.exe" --flag`, `C:\Program Files\Vendor\service.exe`},
		{"unquoted", `C:\Program Files\Vendor\service.exe --flag`, `C:\Program Files\Vendor\service.exe`},
		{"expanded", `%HOSTDECK_JOBS_PROGRAM_ROOT%\agent.exe --flag`, `C:\Program Files\Vendor\agent.exe`},
		{"unicode", `"C:\工具\任务.exe" --flag`, `C:\工具\任务.exe`},
		{"windowsenv", `%SystemRoot%\System32\svchost.exe -k group`, ""},
		{"systemroot", `\SystemRoot\System32\service.exe`, ""},
		{"ntprefix", `\??\` + root + `\System32\service.exe`, ""},
		{"extended", `\\?\` + root + `\System32\service.exe`, ""},
		{"prefix", root + `Old\service.exe`, root + `Old\service.exe`},
		{"inside", volume + `\Applications\..\` + filepath.Base(root) + `\System32\service.exe`, ""},
		{"outside", root + `\..\Tools\agent.exe`, volume + `\Tools\agent.exe`},
		{"unresolved", `%HOSTDECK_JOBS_MISSING_ROOT%\agent.exe`, ""},
		{"relative", `relative\agent.exe`, ""},
	}
	var services []map[string]any
	want := map[string]string{}
	for _, tc := range cases {
		services = append(services, map[string]any{"Name": tc.id, "PathName": tc.command, "State": "Stopped", "StartMode": "Manual"})
		if tc.program != "" {
			want[tc.id] = tc.program
		}
	}
	var view detail.View
	if err := run.Decode(windowsJobsServicesOutput(t, "detail.ps1", false, services), &view); err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, it := range view.Items {
		for _, field := range it.Fields {
			switch field.Name.ByLang["en"] {
			case "Program":
				got[it.ID] = field.Value.String()
			}
		}
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("programs %v, want %v; Windows and unresolved paths must stay out", got, want)
	}

}

func TestWindowsJobsPreserveTheStateSeparatorInPowerShell51(t *testing.T) {
	services := []map[string]any{{"Name": "failed", "PathName": `C:\Tools\service.exe`, "State": "Stopped", "StartMode": "Auto"}}
	var view detail.View
	if err := run.Decode(windowsJobsServicesOutput(t, "detail.ps1", false, services), &view); err != nil {
		t.Fatal(err)
	}
	if len(view.Items) != 1 || view.Items[0].Value.String() != "Stopped · Auto" {
		t.Fatalf("items %+v, want the UTF-8 separator preserved", view.Items)
	}
}
