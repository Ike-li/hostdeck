package local

import (
	"context"
	"fmt"
	"net"
	"time"
)

// CheckHost reports an error unless every address host resolves to is
// loopback, private or link-local. A name that does not resolve within a few
// seconds is refused.
func CheckHost(host string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	addrs, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	if err != nil {
		return err
	}
	if len(addrs) == 0 {
		return fmt.Errorf("%s resolved to no addresses", host)
	}
	for _, a := range addrs {
		if a = a.Unmap(); !a.IsLoopback() && !a.IsPrivate() && !a.IsLinkLocalUnicast() {
			return fmt.Errorf("%s is not on this machine or the local network", host)
		}
	}
	return nil
}
