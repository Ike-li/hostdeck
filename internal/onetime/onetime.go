// Package onetime hands out tokens that work once, for one service, for a
// short while.
package onetime

import (
	"crypto/rand"
	"sync"
	"time"
)

// Lifetime is how long an unused token keeps working.
const Lifetime = time.Minute

type Tokens struct {
	now    func() time.Time
	mu     sync.Mutex
	issued map[string]issued // token -> what it was issued for
}

type issued struct {
	id      string
	expires time.Time
}

func New(now func() time.Time) *Tokens {
	return &Tokens{now: now, issued: map[string]issued{}}
}

// Issue returns a new token for the service id.
func (t *Tokens) Issue(id string) string {
	token := rand.Text()
	t.mu.Lock()
	defer t.mu.Unlock()
	now := t.now()
	for tok, is := range t.issued {
		if now.After(is.expires) {
			delete(t.issued, tok)
		}
	}
	t.issued[token] = issued{id: id, expires: now.Add(Lifetime)}
	return token
}

// Redeem reports whether token was issued for id and is still unused and
// fresh, and uses it up.
func (t *Tokens) Redeem(id, token string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	is, ok := t.issued[token]
	if !ok || is.id != id {
		return false
	}
	delete(t.issued, token)
	return !t.now().After(is.expires)
}
