package manualview

import "time"

// inspection is what could be read about one listener. A false Knows field
// means that part was not read, not that it is empty.
type inspection struct {
	Dir         string
	KnowsDir    bool
	Args        []string
	CommandLine string
	Exe         string
	KnowsArgs   bool
	Env         []string
	KnowsEnv    bool
	Started     time.Time
}

func enrichListed(listeners map[int]Listener, procs map[int]Proc) {
	for _, h := range selectHits(listeners, procs) {
		proc, ok := procs[h.pid]
		if !ok {
			continue
		}
		in := inspect(h.pid)
		if in.KnowsDir {
			proc.Dir, proc.KnowsDir = in.Dir, true
		}
		if in.KnowsArgs {
			proc.Args, proc.KnowsArgs = in.Args, true
			proc.CommandLine, proc.Exe = in.CommandLine, in.Exe
		}
		if in.KnowsEnv {
			if in.Env == nil {
				in.Env = []string{}
			}
			proc.Env, proc.KnowsEnv = in.Env, true
		}
		if !in.Started.IsZero() {
			proc.Started = in.Started
		}
		procs[h.pid] = proc
	}
}
