package detail

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestDetailValuesKeepBothLanguagesAndLegacyNumbers(t *testing.T) {
	printed := `{"items":[{"id":"svc","name":"Service","value":{"en":"Running","zh-CN":"运行中"},"fields":[{"name":"When","value":{"en":"At login","zh-CN":"登录时启动"}},{"name":"Count","value":42}]}]}`
	p, _ := fakePlugin(t, printed, "")
	got := Read(context.Background(), p)
	if got.Error != nil {
		t.Fatalf("reading %+v", got.Error)
	}
	out, err := json.Marshal(got)
	if err != nil || !strings.Contains(string(out), `"zh-CN":"运行中"`) || !strings.Contains(string(out), `"zh-CN":"登录时启动"`) || !strings.Contains(string(out), `"value":"42"`) {
		t.Fatalf("output %s error %v", out, err)
	}
}
