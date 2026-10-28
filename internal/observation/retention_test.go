package observation

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCaptureCapacityRefusesBeforeDeletingAnyActiveRecord(t *testing.T) {
	root := t.TempDir()
	ids := []string{}
	for i := 0; i < 128; i++ {
		id := token()
		err := reserveCapture(root, id, "service")
		if err != nil {
			break
		}
		ids = append(ids, id)
	}
	if len(ids) == 0 || len(ids) >= 128 {
		t.Fatalf("capacity admitted %d", len(ids))
	}
	if reserveCapture(root, token(), "service") == nil {
		t.Fatal("capacity did not refuse")
	}
	for _, id := range ids {
		if _, err := ReadCapture(root, id); err != nil {
			t.Fatal("active record deleted", err)
		}
	}
}
func TestOnlyThreeCompletedCapturesPerServiceSurviveReclamation(t *testing.T) {
	root := t.TempDir()
	var ids []string
	for i := 0; i < 5; i++ {
		id := token()
		if err := reserveCapture(root, id, "service"); err != nil {
			t.Fatal(err)
		}
		record, _ := ReadCapture(root, id)
		record.State = "exited"
		record.Completed = time.Now().UTC().Add(time.Duration(i-10) * time.Hour)
		saveCapture(root, record)
		ids = append(ids, id)
	}
	if err := reserveCapture(root, token(), "another-service"); err != nil {
		t.Fatal(err)
	}
	for i, id := range ids {
		_, err := os.Stat(filepath.Join(root, id))
		if (i < 2) != os.IsNotExist(err) {
			t.Fatalf("record %d: %v", i, err)
		}
	}
}
func TestExpiredCompletedCapturesAreRemovedAndCorruptionRefusesPreparation(t *testing.T) {
	root := t.TempDir()
	id := token()
	reserveCapture(root, id, "service")
	record, _ := ReadCapture(root, id)
	record.State = "exited"
	record.Completed = time.Now().UTC().Add(-8 * 24 * time.Hour)
	saveCapture(root, record)
	if err := reserveCapture(root, token(), "service"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, id)); !os.IsNotExist(err) {
		t.Fatal("expired record retained")
	}
	os.WriteFile(filepath.Join(root, token()+".json"), []byte("corrupt"), 0600)
	if reserveCapture(root, token(), "service") == nil {
		t.Fatal("corrupt records allowed destructive preparation")
	}
}
