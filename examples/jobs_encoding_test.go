package examples

import (
	"context"
	"slices"
	"testing"

	"github.com/Ike-li/hostdeck/internal/detail"
)

func TestJobDetailsPreserveControlCharactersAndUnicodeInPaths(t *testing.T) {
	path := "/tmp/中文 \"quoted\"\\program\tname\r\b\f"
	withJobInfo(t, "\tstate = running\n\tprogram = "+path)
	v := detail.Read(context.Background(), load(t, "jobs"))
	if v.Error != nil || len(v.Items) != 1 {
		t.Fatalf("detail %+v, want valid JSON for a path containing control characters", v)
	}
	if !slices.ContainsFunc(v.Items[0].Fields, func(f detail.Field) bool {
		return f.Name.ByLang["en"] == "Program" && string(f.Value) == path
	}) {
		t.Errorf("fields %+v, want the original path preserved", v.Items[0].Fields)
	}
}
