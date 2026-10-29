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
	"time"
	"unicode"

	"github.com/Ike-li/hostdeck/internal/run"
)

func readPlatform(ctx context.Context, dir string, r run.Runner, metrics bool) ([]proc, error) {
	if r == nil {
		return nil, errors.New("missing process runner")
	}
	env := make([]string, 0, len(os.Environ())+1)
	for _, e := range os.Environ() {
		if !strings.HasPrefix(e, "LC_ALL=") && !strings.HasPrefix(e, "TZ=") {
			env = append(env, e)
		}
	}
	env = append(env, "LC_ALL=C", "TZ=UTC")
	format := "pid=,ppid=,user=,%cpu=,rss=,comm="
	fieldsBeforePath := 5
	if metrics {
		format = "pid=,ppid=,user=,%cpu=,rss=,lstart=,etime=,time=,comm="
		fieldsBeforePath = 12
	}
	out, err := r(ctx, run.Command{Dir: dir, Args: []string{"/bin/ps", "-axo", format}, Env: env})
	if err != nil {
		return nil, err
	}
	if len(out) >= 4<<20 {
		return nil, errors.New("process table exceeds output limit")
	}
	procs := []proc{}
	seen := map[int]bool{}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		fields := make([]string, 0, fieldsBeforePath)
		rest := strings.TrimSpace(line)
		for range fieldsBeforePath {
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
		p := proc{PID: pid, PPID: ppid, Name: filepath.Base(rest), User: fields[2], CPU: &cpu, Memory: &memory}
		if metrics {
			if filepath.IsAbs(rest) {
				p.Path = rest
			}
			p.StartedAt, _ = time.Parse("Mon Jan 2 15:04:05 2006", strings.Join(fields[5:10], " "))
			p.Elapsed = parseProcessDuration(fields[10])
			p.CPUTime = parseProcessDuration(fields[11])
		}
		procs = append(procs, p)
	}
	if metrics && ctx.Err() == nil {
		counts := readThreadCounts(ctx, dir, r, env)
		for i := range procs {
			if count, ok := counts[procs[i].PID]; ok {
				procs[i].Threads = &count
			}
		}
	}
	return procs, nil
}

// Thread enumeration is optional and bounded independently of the main table.
// top performs one batch read rather than starting a helper for every PID.
func readThreadCounts(ctx context.Context, dir string, r run.Runner, env []string) map[int]uint32 {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	const limit = 1 << 20
	out, err := r(ctx, run.Command{Dir: dir, Args: []string{"/usr/bin/top", "-l", "1", "-ncols", "80", "-stats", "pid,threads", "-R", "-F"}, Env: env, Limit: limit})
	if err != nil || ctx.Err() != nil || len(out) >= limit {
		return nil
	}
	header := false
	counts := map[int]uint32{}
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if !header {
			if len(fields) == 2 && fields[0] == "PID" && fields[1] == "#TH" {
				header = true
			}
			continue
		}
		if len(fields) != 2 {
			return nil
		}
		pid, err := strconv.Atoi(fields[0])
		if err != nil || pid < 0 {
			return nil
		}
		parts := strings.Split(fields[1], "/")
		if len(parts) > 2 {
			return nil
		}
		n, err := strconv.ParseUint(parts[0], 10, 32)
		if err != nil {
			return nil
		}
		if len(parts) == 2 {
			running, err := strconv.ParseUint(parts[1], 10, 32)
			if err != nil || running > n {
				return nil
			}
		}
		if _, duplicate := counts[pid]; duplicate {
			return nil
		}
		counts[pid] = uint32(n)
	}
	if !header {
		return nil
	}
	return counts
}
