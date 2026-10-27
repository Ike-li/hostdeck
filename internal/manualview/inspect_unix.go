//go:build darwin

package manualview

import (
	"bytes"
	"context"
	"encoding/binary"
	"os/exec"
	"strconv"
	"time"

	"golang.org/x/sys/unix"
)

const (
	maxInspectArgs = 1 << 20
	maxInspectEnv  = 256 << 10
)

func inspect(pid int) inspection {
	var out inspection
	if info, err := unix.SysctlKinfoProc("kern.proc.pid", pid); err == nil {
		out.Started = time.Unix(info.Proc.P_starttime.Sec, int64(info.Proc.P_starttime.Usec)*1000)
	}
	if buf, err := unix.SysctlRaw("kern.procargs2", pid); err == nil && len(buf) <= maxInspectArgs {
		if exe, args, env, ok := parseProcArgs(buf); ok {
			out.Args, out.KnowsArgs = args, true
			out.Exe = exe
			if len(env) > 0 || env != nil {
				size := 0
				for _, e := range env {
					size += len(e)
				}
				if size <= maxInspectEnv {
					out.Env, out.KnowsEnv = env, true
				}
			}
		}
	}
	if dir, ok := cwdOf(pid); ok {
		out.Dir, out.KnowsDir = dir, true
	}
	return out
}

func parseProcArgs(buf []byte) (exe string, args, env []string, ok bool) {
	if len(buf) < 4 {
		return "", nil, nil, false
	}
	argc := int(binary.LittleEndian.Uint32(buf[:4]))
	if argc <= 0 || argc > 1<<16 {
		return "", nil, nil, false
	}
	buf = buf[4:]
	i := bytes.IndexByte(buf, 0)
	if i < 0 {
		return "", nil, nil, false
	}
	if i > 0 && buf[0] == '/' {
		exe = string(buf[:i])
	}
	buf = buf[i+1:]
	for len(buf) > 0 && buf[0] == 0 {
		buf = buf[1:]
	}
	args = make([]string, 0, argc)
	for n := 0; n < argc; n++ {
		i = bytes.IndexByte(buf, 0)
		if i < 0 {
			return "", nil, nil, false
		}
		args = append(args, string(buf[:i]))
		buf = buf[i+1:]
	}
	env = []string{}
	for len(buf) > 0 && buf[0] != 0 {
		i = bytes.IndexByte(buf, 0)
		if i < 0 {
			break
		}
		env = append(env, string(buf[:i]))
		buf = buf[i+1:]
	}
	return exe, args, env, true
}

func cwdOf(pid int) (string, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/usr/sbin/lsof", "-nP", "-a", "-p", strconv.Itoa(pid), "-d", "cwd", "-F", "n")
	out, _ := cmd.Output()
	return parseCwd(out)
}

func parseCwd(out []byte) (string, bool) {
	var dir string
	for _, line := range bytes.Split(out, []byte("\n")) {
		if len(line) > 1 && line[0] == 'n' {
			dir = string(line[1:])
		}
	}
	if len(dir) == 0 || dir[0] != '/' {
		return "", false
	}
	return dir, true
}
