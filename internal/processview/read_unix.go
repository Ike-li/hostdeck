//go:build unix

package processview

import (
	"context"
	"errors"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"

	"github.com/Ike-li/hostdeck/internal/run"
)

func readPlatform(ctx context.Context, dir string, r run.Runner, _ bool) ([]proc, error) {
	if r == nil {
		return nil, errors.New("missing process runner")
	}
	env := make([]string, 0, len(os.Environ())+1)
	for _, e := range os.Environ() {
		if !strings.HasPrefix(e, "LC_ALL=") {
			env = append(env, e)
		}
	}
	env = append(env, "LC_ALL=C")
	out, err := r(ctx, run.Command{Dir: dir, Args: []string{"/bin/ps", "-axo", "pid=,ppid=,user=,%cpu=,rss=,comm="}, Env: env})
	if err != nil {
		return nil, err
	}
	if len(out) >= 4<<20 {
		return nil, errors.New("process table exceeds output limit")
	}
	procs := []proc{}
	seen := map[int]bool{}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		fields := make([]string, 0, 5)
		rest := strings.TrimSpace(line)
		for range 5 {
			pos := strings.IndexFunc(rest, unicode.IsSpace)
			if pos < 0 {
				return nil, errors.New("invalid process table")
			}
			fields = append(fields, rest[:pos])
			rest = strings.TrimSpace(rest[pos:])
		}
		pid, e1 := strconv.Atoi(fields[0])
		ppid, e2 := strconv.Atoi(fields[1])
		cpu, e3 := strconv.ParseFloat(fields[3], 64)
		rss, e4 := strconv.ParseUint(fields[4], 10, 64)
		if e1 != nil || e2 != nil || e3 != nil || e4 != nil || pid < 0 || ppid < 0 || seen[pid] || rest == "" || cpu < 0 || math.IsNaN(cpu) || math.IsInf(cpu, 0) || rss > math.MaxUint64/1024 {
			return nil, errors.New("invalid process table")
		}
		seen[pid] = true
		memory := rss * 1024
		procs = append(procs, proc{PID: pid, PPID: ppid, Name: filepath.Base(rest), User: fields[2], CPU: &cpu, Memory: &memory})
	}
	return procs, nil
}
