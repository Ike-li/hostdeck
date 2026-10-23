package web

import (
	"encoding/json"
	"maps"
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

func TestAgentPromptsClarifyPageIDsExecKeysOptionalFieldsAndVerification(t *testing.T) {
	for _, tc := range []struct {
		path string
		want map[string][]string
	}{
		{
			path: "i18n/en.json",
			want: map[string][]string{
				"help.agent.pages":    {"data.id", "page update --url ORIGIN --id ID", "remove the temporary backup"},
				"help.agent.contract": {"card.exec.command", "detail.exec.command", "actions.exec.command", "exec.windows", "fields, log and actions are optional"},
				"help.agent.finish":   {"GET /api/cards", "GET /api/detail/<id>", "monitoring card", "detail"},
			},
		},
		{
			path: "i18n/zh-CN.json",
			want: map[string][]string{
				"help.agent.pages":    {"data.id", "page update --url ORIGIN --id ID", "删除临时备份"},
				"help.agent.contract": {"card.exec.command", "detail.exec.command", "actions.exec.command", "exec.windows", "fields、log、actions 可选"},
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
