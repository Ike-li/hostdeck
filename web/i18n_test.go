package web

import (
	"encoding/json"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

func keys(t *testing.T, path string) []string {
	t.Helper()
	data, err := Files.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var dict map[string]string
	if err := json.Unmarshal(data, &dict); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return slices.Sorted(maps.Keys(dict))
}

func TestBothLanguagesHaveTheSameKeys(t *testing.T) {
	en, zh := keys(t, "i18n/en.json"), keys(t, "i18n/zh-CN.json")
	for _, k := range en {
		if !slices.Contains(zh, k) {
			t.Errorf("zh-CN.json lacks %q", k)
		}
	}
	for _, k := range zh {
		if !slices.Contains(en, k) {
			t.Errorf("en.json lacks %q", k)
		}
	}
}

func dict(t *testing.T, path string) map[string]string {
	t.Helper()
	data, err := Files.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]string
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return m
}

var errorCode = regexp.MustCompile(`Code:\s*"([a-z_]+)"`)

// errorCodes reads every code the Go side answers an error with. A code added
// to a handler cannot reach the page unworded: explain() would fall back to
// the English detail the server sent.
func errorCodes(t *testing.T) []string {
	t.Helper()
	var codes []string
	seen := map[string]bool{}
	err := filepath.WalkDir("../internal", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, m := range errorCode.FindAllStringSubmatch(string(data), -1) {
			if !seen[m[1]] {
				seen[m[1]] = true
				codes = append(codes, m[1])
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(codes)
	return codes
}

func TestEveryErrorCodeTheBackendAnswersIsWorded(t *testing.T) {
	en, zh := dict(t, "i18n/en.json"), dict(t, "i18n/zh-CN.json")
	for _, code := range errorCodes(t) {
		// reported carries a plugin's own words, which explain() reads
		// straight from the error instead of from the dictionary.
		if code == "reported" {
			continue
		}
		if _, ok := en["error."+code]; !ok {
			t.Errorf("en.json lacks error.%s", code)
		}
		if _, ok := zh["error."+code]; !ok {
			t.Errorf("zh-CN.json lacks error.%s", code)
		}
	}
}

func TestAgentPromptsClarifyPageIDsExecKeysOptionalFieldsAndVerification(t *testing.T) {
	for _, tc := range []struct {
		path string
		want map[string][]string
	}{
		{
			path: "i18n/en.json",
			want: map[string][]string{
				"help.agent.pages":    {"data.id", "page update --url ORIGIN --id ID", "remove the temporary backup"},
				"help.agent.contract": {"card.exec.command", "detail.exec.command", "actions.exec.command", "exec.windows", "fields, log, logAvailable and actions are optional", "logAvailable true means a log can be loaded for that item", "HOSTDECK_DETAIL_ITEM", "items[].label"},
				"help.agent.finish":   {"GET /api/cards", "GET /api/detail/<id>", "monitoring card", "detail"},
			},
		},
		{
			path: "i18n/zh-CN.json",
			want: map[string][]string{
				"help.agent.pages":    {"data.id", "page update --url ORIGIN --id ID", "删除临时备份"},
				"help.agent.contract": {"card.exec.command", "detail.exec.command", "actions.exec.command", "exec.windows", "fields、log、logAvailable、actions 可选", "logAvailable 为 true 表示可以再读取该条目的日志", "HOSTDECK_DETAIL_ITEM", "items[].label"},
				"help.agent.finish":   {"GET /api/cards", "GET /api/detail/<id>", "监控卡片", "详情"},
			},
		},
	} {
		m := dict(t, tc.path)
		for key, subs := range tc.want {
			for _, sub := range subs {
				if !strings.Contains(m[key], sub) {
					t.Errorf("%s[%q] lacks %q", tc.path, key, sub)
				}
			}
		}
	}
}

// The rules that make a plugin program fail are the ones an agent cannot guess
// from an example; docs/PLUGINS.md gives them in full.
func TestTheAgentPromptSaysWhatMakesAPluginProgramFail(t *testing.T) {
	for path, subs := range map[string][]string{
		"i18n/en.json":    {"exactly one JSON value", "unknown field", "exit code other than 0", "30 seconds", `{"error":"Reason"} with exit code 0`, "id is required and unique; name is required and must contain nonblank text", "localized text"},
		"i18n/zh-CN.json": {"恰好一个 JSON 值", "不认识的字段", "退出码不为 0", "30 秒", `{"error":"失败原因"}，退出码仍为 0`, "id 必填且各不相同；name 必填且须含非空白文字", "本地化文字"},
	} {
		contract := dict(t, path)["help.agent.contract"]
		for _, sub := range subs {
			if !strings.Contains(contract, sub) {
				t.Errorf("%s help.agent.contract lacks %q", path, sub)
			}
		}
	}
}
