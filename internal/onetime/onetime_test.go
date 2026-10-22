package onetime

import (
	"testing"
	"time"
)

func TestATokenWorksOnceAndOnlyForItsService(t *testing.T) {
	tokens := New(time.Now)
	tok := tokens.Issue("b")

	if tokens.Redeem("c", tok) {
		t.Error("a token for b worked for c")
	}
	if !tokens.Redeem("b", tok) {
		t.Error("a fresh token for b did not work for b")
	}
	if tokens.Redeem("b", tok) {
		t.Error("a token worked twice")
	}
	if tokens.Redeem("b", "") || tokens.Redeem("b", "guess") {
		t.Error("a token that was never issued worked")
	}
}

func TestATokenExpires(t *testing.T) {
	now := time.Now()
	tokens := New(func() time.Time { return now })
	tok := tokens.Issue("b")

	now = now.Add(Lifetime + time.Second)
	if tokens.Redeem("b", tok) {
		t.Errorf("a token worked after %v", Lifetime)
	}
}
