package examples

import (
	"context"
	"os"
	"strconv"
	"testing"

	"github.com/Ike-li/hostdeck/internal/card"
	"github.com/Ike-li/hostdeck/internal/detail"
)

func TestTheProcessesPluginIsReadOnlyAndUsesNativeReaders(t *testing.T) {
	p := load(t, "processes")
	if p.Actions != nil || p.Detail == nil || p.Card == nil {
		t.Fatalf("plugin %+v", p)
	}
	if got := p.Detail.Exec.Args(); len(got) != 3 || got[0] != "@hostdeck" || got[1] != "processes" || got[2] != "detail" {
		t.Fatalf("detail command %v", got)
	}
}

func TestTheInstalledProcessPluginReadsTheCurrentProcess(t *testing.T) {
	if testing.Short() {
		t.Skip("reads real processes")
	}
	p := load(t, "processes")
	v := detail.Read(context.Background(), p)
	if v.Error != nil {
		t.Fatalf("detail error %+v", v.Error)
	}
	found := false
	for _, item := range v.Items {
		if item.ID == strconv.Itoa(os.Getpid()) {
			found = true
		}
		if len(item.Actions) > 0 {
			t.Fatalf("process offers actions %+v", item)
		}
	}
	if !found {
		t.Fatal("test process is missing")
	}
	c := card.Fetch(context.Background(), p)
	count, err := strconv.Atoi(c.Value)
	if c.Error != nil || err != nil || count < 1 || c.Status != "" {
		t.Fatalf("card %+v", c)
	}
}
