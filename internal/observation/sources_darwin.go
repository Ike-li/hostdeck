package observation

import (
	"bytes"
	"context"
	"os/exec"
	"strconv"
	"time"
)

func ProcessFiles(ctx context.Context, identity Identity) ([]string, string) {
	if !identity.Equal(ProcessIdentity(identity.PID)) {
		return nil, "identity_changed"
	}
	bounded, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	out, err := exec.CommandContext(bounded, "/usr/sbin/lsof", "-nP", "-a", "-p", strconv.Itoa(identity.PID), "-d", "1,2", "-Fftn").Output()
	if !identity.Equal(ProcessIdentity(identity.PID)) {
		return nil, "identity_changed"
	}
	if err != nil {
		return nil, "external"
	}
	paths := descriptorFiles(out)
	if len(paths) > 0 {
		return paths, ""
	}
	// A character device is deliberately never opened. Report the original
	// terminal separately from processes without an identifiable source.
	if bytes.Contains(out, []byte("tCHR\n")) {
		return nil, "terminal"
	}
	return nil, "external"
}
