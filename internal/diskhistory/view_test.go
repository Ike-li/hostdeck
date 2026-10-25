package diskhistory

import (
	"encoding/json"
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
