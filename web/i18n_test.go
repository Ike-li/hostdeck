package web

import (
	"encoding/json"
	"maps"
	"slices"
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
