package tasks

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestTheTaskReferenceNamesEveryConfigurationAndStoredField(t *testing.T) {
	var configFields func(reflect.Type, string) []string
	configFields = func(typ reflect.Type, prefix string) []string {
		var fields []string
		for i := 0; i < typ.NumField(); i++ {
			f := typ.Field(i)
			tag, _, _ := strings.Cut(f.Tag.Get("json"), ",")
			if tag == "" || tag == "-" {
				continue
			}
			path := prefix + tag
			next := f.Type
			if next.Kind() == reflect.Slice && next.Elem().Kind() == reflect.Struct {
				fields = append(fields, path)
				fields = append(fields, configFields(next.Elem(), path+"[].")...)
			} else if next.Kind() == reflect.Struct {
				fields = append(fields, configFields(next, path+".")...)
			} else {
				fields = append(fields, path)
			}
		}
		return fields
	}
	fields := configFields(reflect.TypeFor[Config](), "")
	for _, typ := range []reflect.Type{reflect.TypeFor[saved](), reflect.TypeFor[taskState](), reflect.TypeFor[Record](), reflect.TypeFor[ScriptRun](), reflect.TypeFor[logs]()} {
		for i := 0; i < typ.NumField(); i++ {
			tag, _, _ := strings.Cut(typ.Field(i).Tag.Get("json"), ",")
			fields = append(fields, tag)
		}
	}
	for _, doc := range []string{"TASKS.md", "TASKS.zh-CN.md"} {
		b, err := os.ReadFile(filepath.Join("..", "..", "docs", doc))
		if err != nil {
			t.Fatal(err)
		}
		for _, field := range fields {
			if !strings.Contains(string(b), "`"+field+"`") {
				t.Errorf("%s does not name %s", doc, field)
			}
		}
	}
}

func TestCronUsesDayOrRulesAndFindsLeapDays(t *testing.T) {
	for _, tc := range []struct{ cron, after, next string }{
		{"0 0 13 * 1", "2026-01-12T00:00:00Z", "2026-01-13T00:00:00Z"},
		{"0 0 13 * 1", "2026-01-13T00:00:00Z", "2026-01-19T00:00:00Z"},
		{"0 0 29 2 *", "2026-01-01T00:00:00Z", "2028-02-29T00:00:00Z"},
		{"*/15 9-17/2 * * 7", "2026-01-03T23:59:00Z", "2026-01-04T09:00:00Z"},
	} {
		t.Run(tc.cron+tc.after, func(t *testing.T) {
			zone := "UTC"
			s := Schedule{Cron: &tc.cron, Timezone: &zone}
			if err := s.validate(); err != nil {
				t.Fatal(err)
			}
			after, _ := time.Parse(time.RFC3339, tc.after)
			if got := s.next(after, nil).Format(time.RFC3339); got != tc.next {
				t.Fatalf("next %s want %s", got, tc.next)
			}
		})
	}
}
