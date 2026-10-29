package examples

import (
	"context"
	"encoding/json"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
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
		return f.Name.ByLang["en"] == "Program" && f.Value.String() == path
	}) {
		t.Errorf("fields %+v, want the original path preserved", v.Items[0].Fields)
	}
}

func TestShortJobTextKeepsEveryJSONControlCharacterWithoutStartingAnEncoder(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS shell")
	}
	path, err := filepath.Abs("jobs/launchd.sh")
	if err != nil {
		t.Fatal(err)
	}
	var controls strings.Builder
	for i := 1; i < 32; i++ {
		controls.WriteByte(byte(i))
	}
	for _, input := range []string{"", "中文 🐈 \"quoted\" \\ $() ' \n", controls.String() + "\n\n"} {
		// A short value must not need the awk fallback at all.
		out, err := exec.Command("sh", "-c", `. "$1"; jobs_escape_awk() { exit 91; }; jobs_escape "$2"`, "sh", path, input).Output()
		if err != nil {
			t.Fatal(err)
		}
		var decoded string
		if err = json.Unmarshal(append(append([]byte{'"'}, out...), '"'), &decoded); err != nil || decoded != input {
			t.Fatalf("round trip %q => %q: %v", input, decoded, err)
		}
	}
}
