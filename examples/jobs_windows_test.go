package examples

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/Ike-li/hostdeck/internal/card"
	"github.com/Ike-li/hostdeck/internal/detail"
	"github.com/Ike-li/hostdeck/internal/run"
)

// Only the OS query is replaced. The shipped PowerShell scripts still parse,
// filter and classify the services, and encode their actual card/detail JSON.
func windowsJobsOutput(t *testing.T, script string, failed bool) []byte {
	t.Helper()
	root := os.Getenv("SystemRoot")
	services := []map[string]any{
		{"Name": "running", "DisplayName": "Running job", "PathName": `C:\Tools\run.exe`, "AcceptStop": true, "StartMode": "Auto", "State": "Running", "ExitCode": "255"},
		{"Name": "idle", "DisplayName": "Idle job", "PathName": `C:\Tools\idle.exe`, "State": "Stopped", "StartMode": "Manual"},
		{"Name": "failed", "DisplayName": "Failed job", "PathName": `C:\Tools\failed.exe`, "State": "Stopped", "StartMode": "Auto", "ExitCode": "1"},
		{"Name": "disabled", "PathName": `C:\Tools\disabled.exe`, "State": "Stopped", "StartMode": "Disabled"},
		{"Name": "paused", "PathName": `C:\Tools\paused.exe`, "AcceptStop": true, "StartMode": "Manual", "State": "Paused"},
		{"Name": "pending", "PathName": `C:\Tools\pending.exe`, "State": "Start Pending"},
		{"Name": "unsafe/name", "PathName": `C:\Tools\unsafe.exe`, "AcceptStop": true, "StartMode": "Manual", "State": "Running"},
		{"Name": "quoted", "DisplayName": "中文 \"service\"", "PathName": `"C:\Program Files\Vendor\service.exe" --flag`, "AcceptStop": true, "StartMode": "Manual", "State": "Running"},
		{"Name": "prefix", "PathName": root + `Old\service.exe`, "AcceptStop": true, "StartMode": "Manual", "State": "Running"},
		{"Name": "windows", "PathName": `"` + root + `\System32\svchost.exe" -k group`, "AcceptStop": true, "StartMode": "Manual", "State": "Running"},
		{"Name": "missing", "PathName": "", "AcceptStop": true, "StartMode": "Manual", "State": "Running"},
	}
	return windowsJobsServicesOutput(t, script, failed, services)
}

func windowsJobsServicesOutput(t *testing.T, script string, failed bool, services []map[string]any) []byte {
	return windowsJobsServicesAndEventsOutput(t, script, failed, services, `return @()`)
}

func windowsJobsServicesAndEventsOutput(t *testing.T, script string, failed bool, services []map[string]any, eventScript string) []byte {
	t.Helper()
	data, err := json.Marshal(services)
	if err != nil {
		t.Fatal(err)
	}
	body := "$ErrorActionPreference = 'Stop'\nfunction Get-CimInstance { param($ClassName, $ErrorAction)\n"
	if failed {
		body += "throw 'fixture query refused'\n"
	} else {
		body += "$items = @'\n" + string(data) + "\n'@ | ConvertFrom-Json\nforeach ($item in $items) { $item }\n"
	}
	body += "}\nfunction Get-WinEvent { param($FilterHashtable,$MaxEvents,$ErrorAction)\n" + eventScript + "\n"
	path, err := filepath.Abs(filepath.Join("jobs", script))
	if err != nil {
		t.Fatal(err)
	}
	body += "}\n. '" + strings.ReplaceAll(path, "'", "''") + "'\n"
	wrapper := filepath.Join(t.TempDir(), "query.ps1")
	if err := os.WriteFile(wrapper, append([]byte{0xef, 0xbb, 0xbf}, []byte(body)...), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	out, err := run.Output(ctx, ".", []string{"powershell", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-File", wrapper})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestWindowsJobsClassifyFixedServicesAndOfferOnlyAllowedActions(t *testing.T) {
	var got struct {
		Status string          `json:"status"`
		Value  string          `json:"value"`
		Rows   []card.Row      `json:"rows"`
		Label  json.RawMessage `json:"label"`
	}
	if err := run.Decode(windowsJobsOutput(t, "card.ps1", false), &got); err != nil {
		t.Fatal(err)
	}
	if got.Status != "error" || got.Value != "3" || len(got.Rows) != 3 {
		t.Fatalf("card %+v, want three jobs needing attention: one error and two warnings", got)
	}
	for i, want := range []string{"error", "warn", "warn"} {
		if got.Rows[i].Status != want {
			t.Errorf("row %d: %+v, want %s", i, got.Rows[i], want)
		}
	}
	var view detail.View
	if err := run.Decode(windowsJobsOutput(t, "detail.ps1", false), &view); err != nil {
		t.Fatal(err)
	}
	var ids, states, actions []string
	for _, item := range view.Items {
		ids = append(ids, item.ID)
		states = append(states, item.Status)
		action := ""
		if len(item.Actions) > 1 {
			t.Fatalf("too many actions: %+v", item)
		}
		if len(item.Actions) == 1 {
			action = item.Actions[0].ID
		}
		actions = append(actions, action)
	}
	if want := []string{"running", "idle", "failed", "disabled", "paused", "pending", "unsafe/name", "quoted", "prefix"}; !reflect.DeepEqual(ids, want) {
		t.Fatalf("IDs %v, want %v", ids, want)
	}
	if want := []string{"ok", "ok", "error", "ok", "warn", "warn", "ok", "ok", "ok"}; !reflect.DeepEqual(states, want) {
		t.Errorf("states %v, want %v", states, want)
	}
	if want := []string{"restart", "start", "start", "", "restart", "", "", "restart", "restart"}; !reflect.DeepEqual(actions, want) {
		t.Errorf("actions %v, want %v", actions, want)
	}
	if view.Items[7].Name.All != "中文 \"service\"" {
		t.Errorf("name %+v, want Unicode and quotes preserved", view.Items[7].Name)
	}
}

func TestWindowsJobsExplainARefusedSystemQuery(t *testing.T) {
	for _, script := range []string{"card.ps1", "detail.ps1"} {
		var got struct {
			Error map[string]string `json:"error"`
		}
		if err := run.Decode(windowsJobsOutput(t, script, true), &got); err != nil {
			t.Fatal(err)
		}
		for _, lang := range []string{"en", "zh-CN"} {
			if !strings.Contains(got.Error[lang], "fixture query refused") {
				t.Errorf("%s %s: %+v", script, lang, got)
			}
		}
	}
}

func TestWindowsJobsNeverOfferRestartForDisabledOrUncontrollableServices(t *testing.T) {
	services := []map[string]any{}
	for _, state := range []string{"Running", "Paused", "Start Pending", "Stop Pending", "Continue Pending", "Pause Pending", "Unknown"} {
		for _, mode := range []string{"Disabled", "Auto"} {
			services = append(services, map[string]any{"Name": strings.ReplaceAll(state, " ", "") + mode, "PathName": `C:\Tools\service.exe`, "State": state, "StartMode": mode, "AcceptStop": false})
		}
	}
	for _, state := range []string{"Start Pending", "Stop Pending", "Continue Pending", "Pause Pending", "Unknown"} {
		services = append(services, map[string]any{"Name": strings.ReplaceAll(state, " ", ""), "PathName": `C:\Tools\service.exe`, "State": state, "StartMode": "Auto", "AcceptStop": true})
	}
	// Disabled services must also be blocked when the current instance accepts Stop.
	services = append(services, map[string]any{"Name": "disabledrunning", "PathName": `C:\Tools\service.exe`, "State": "Running", "StartMode": "Disabled", "AcceptStop": true})
	var view detail.View
	if err := run.Decode(windowsJobsServicesOutput(t, "detail.ps1", false, services), &view); err != nil {
		t.Fatal(err)
	}
	if len(view.Items) != len(services) {
		t.Fatalf("items %+v, want every service retained", view.Items)
	}
	for _, item := range view.Items {
		if len(item.Actions) != 0 {
			t.Errorf("item %+v must not offer an unavailable action", item)
		}
	}
}

func TestWindowsJobsCountWarningsEvenWhenThereAreNoErrors(t *testing.T) {
	services := []map[string]any{
		{"Name": "paused", "PathName": `C:\Tools\paused.exe`, "State": "Paused", "StartMode": "Manual", "AcceptStop": true},
		{"Name": "pending", "PathName": `C:\Tools\pending.exe`, "State": "Start Pending", "StartMode": "Auto", "AcceptStop": false},
	}
	var got struct {
		Status string          `json:"status"`
		Value  string          `json:"value"`
		Rows   []card.Row      `json:"rows"`
		Label  json.RawMessage `json:"label"`
	}
	if err := run.Decode(windowsJobsServicesOutput(t, "card.ps1", false, services), &got); err != nil {
		t.Fatal(err)
	}
	if got.Status != "warn" || got.Value != "2" || len(got.Rows) != 2 {
		t.Fatalf("card %+v, want two warning jobs needing attention", got)
	}
}

func TestWindowsJobDetailsKeepServiceIdentityAndBoundedMatchingEvents(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows service reader")
	}
	services := []map[string]any{{"Name": "service-id", "DisplayName": "服务名称", "PathName": `C:\Tools\run.exe --flag`, "State": "Running", "StartMode": "Auto", "StartName": `LocalSystem`, "ProcessId": 4242, "AcceptStop": true}}
	eventScript := `if ($MaxEvents -ne 200) { throw 'unbounded event query' }
 [pscustomobject]@{TimeCreated='2026-10-26T00:00:00Z';Id=7036;Message='matched event 中文';Properties=@([pscustomobject]@{Value='service-id'})}
 [pscustomobject]@{TimeCreated='2026-10-26T00:00:00Z';Id=7036;Message='unrelated service-id message';Properties=@([pscustomobject]@{Value='another-service'})}`
	out := windowsJobsServicesAndEventsOutput(t, "detail.ps1", false, services, eventScript)
	var view detail.View
	if err := run.Decode(out, &view); err != nil {
		t.Fatal(err)
	}
	if len(view.Items) != 1 {
		t.Fatalf("items %+v", view)
	}
	encoded, _ := json.Marshal(view.Items[0])
	all := string(encoded)
	for _, want := range []string{"service-id", "4242", "LocalSystem", "--flag", "matched event 中文"} {
		if !strings.Contains(all, want) {
			t.Errorf("missing %q: %s", want, all)
		}
	}
	if strings.Contains(view.Items[0].Log, "unrelated") {
		t.Fatal("associated a message-only match with the service")
	}
	out = windowsJobsServicesAndEventsOutput(t, "detail.ps1", false, services, `throw 'fixture event access refused'`)
	if err := run.Decode(out, &view); err != nil {
		t.Fatal(err)
	}
	if len(view.Items) != 1 || len(view.Items[0].Actions) != 1 || !strings.Contains(string(out), "fixture event access refused") {
		t.Fatalf("event failure lost the task: %s", out)
	}
}
