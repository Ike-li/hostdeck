package diskhistory

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// cardFor words the card for saved history at a given moment.
func cardFor(t *testing.T, s saved, targets []string, now time.Time) map[string]json.RawMessage {
	t.Helper()
	data, err := json.Marshal((&entry{state: s}).card(targets, now))
	if err != nil {
		t.Fatal(err)
	}
	var card map[string]json.RawMessage
	if err := json.Unmarshal(data, &card); err != nil {
		t.Fatal(err)
	}
	return card
}

func reading(at time.Time, bytes int64) snapshot {
	return snapshot{Time: at, Sizes: map[string]measurement{".": {Bytes: bytes}}, Allocated: true}
}

func TestASingleReadingSaysHowToSeeChangesWithoutAWarning(t *testing.T) {
	now := time.Now()
	for tracking, want := range map[bool]string{false: "已扫描 1 次 · 开启每小时跟踪后显示变化", true: "已扫描 1 次 · 下次每小时扫描后显示变化"} {
		card := cardFor(t, saved{Version: 1, Tracking: tracking, Scans: map[string][]snapshot{"/target": {reading(now.Add(-time.Minute), 1000)}}}, []string{"/target"}, now)
		if string(card["status"]) != `"ok"` || string(card["value"]) != `"1.00 KB"` || !strings.Contains(string(card["label"]), want) {
			t.Errorf("tracking=%v: %s", tracking, card)
		}
	}
}

func TestAnOldReadingIsAWarningOnlyWhileHourlyTrackingShouldHaveReplacedIt(t *testing.T) {
	now := time.Now()
	for tracking, want := range map[bool]string{false: `"ok"`, true: `"warn"`} {
		card := cardFor(t, saved{Version: 1, Tracking: tracking, Scans: map[string][]snapshot{"/target": {reading(now.Add(-5*time.Hour), 1000)}}}, []string{"/target"}, now)
		if string(card["status"]) != want || !strings.Contains(string(card["label"]), "最后扫描于 5 小时前") {
			t.Errorf("tracking=%v: %s, want status %s", tracking, card, want)
		}
	}
}

func TestTheChangeIsWordedOverWholeHoursOrMinutes(t *testing.T) {
	now := time.Now()
	for span, want := range map[time.Duration][]string{
		23*time.Hour + 36*time.Minute: {"过去 24 小时 · 共 3.00 KB", "Past 24 hours · 3.00 KB total"},
		25 * time.Minute:              {"过去 25 分钟 · 共 3.00 KB", "Past 25 minutes · 3.00 KB total"},
	} {
		card := cardFor(t, saved{Version: 1, Scans: map[string][]snapshot{"/target": {reading(now.Add(-span), 1000), reading(now, 3000)}}}, []string{"/target"}, now)
		if string(card["value"]) != `"+2.00 KB"` || !strings.Contains(string(card["label"]), want[0]) || !strings.Contains(string(card["label"]), want[1]) {
			t.Errorf("%s apart: %s, want %q", span, card, want)
		}
	}
}

func TestTheHomeFolderIsNamedOnTheCard(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	now := time.Now()
	base, latest := reading(now.Add(-24*time.Hour), 1000), reading(now, 5096)
	base.Sizes["code"], latest.Sizes["code"] = measurement{Bytes: 1000}, measurement{Bytes: 5096}
	card := cardFor(t, saved{Version: 1, Scans: map[string][]snapshot{home: {base, latest}}}, []string{home}, now)
	row, _ := json.Marshal(row{filepath.Join("~", "code"), "+4.10 KB"})
	if !strings.Contains(string(card["label"]), "过去 24 小时 · 主目录共 5.10 KB") || !strings.Contains(string(card["label"]), "5.10 KB in the home folder") || !strings.Contains(string(card["rows"]), string(row)) {
		t.Fatalf("home card %s, want row %s", card, row)
	}
}
