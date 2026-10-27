package manualview

import (
	"bytes"
	"encoding/binary"
	"errors"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// commandOutcome is one finished command. Exit is 0 when Err is set, and when
// the command succeeded. lsof exits 1 when nothing is listening.
type commandOutcome struct {
	Out  []byte
	Exit int
	Err  error
}

// readFailure is a bilingual reason a reading cannot be shown. It is not an
// empty list: an empty list means nothing was started by hand.
type readFailure struct {
	en, zh string
}

func (e *readFailure) Error() string { return e.en }

func snapshotFrom(lsof, ps commandOutcome) (map[int]Listener, map[int]Proc, error) {
	if ps.Err != nil {
		return nil, nil, &readFailure{en: "Could not list processes: ps was not found.", zh: "读不到进程列表：找不到 ps。"}
	}
	if ps.Exit != 0 {
		return nil, nil, &readFailure{
			en: "Could not list processes (exit status " + strconv.Itoa(ps.Exit) + ").",
			zh: "读不到进程列表（退出码 " + strconv.Itoa(ps.Exit) + "）。",
		}
	}
	procs := parsePS(ps.Out)
	if len(procs) == 0 {
		return nil, nil, &readFailure{en: "Could not list processes.", zh: "读不到进程列表。"}
	}
	if lsof.Err != nil {
		return nil, nil, &readFailure{en: "Could not list listening ports: lsof was not found.", zh: "读不到正在监听的端口：找不到 lsof。"}
	}
	if lsof.Exit != 0 && len(bytes.TrimSpace(lsof.Out)) == 0 {
		if lsof.Exit == 1 {
			return map[int]Listener{}, procs, nil
		}
		return nil, nil, &readFailure{
			en: "Could not list listening ports (exit status " + strconv.Itoa(lsof.Exit) + ").",
			zh: "读不到正在监听的端口（退出码 " + strconv.Itoa(lsof.Exit) + "）。",
		}
	}
	return parseLsof(lsof.Out), procs, nil
}

func parseLsof(out []byte) map[int]Listener {
	listeners := map[int]Listener{}
	pid := 0
	have := false
	for _, line := range strings.Split(string(out), "\n") {
		if line == "" {
			continue
		}
		tag, value := line[0], line[1:]
		switch tag {
		case 'p':
			n, err := strconv.Atoi(value)
			if err != nil {
				have = false
				continue
			}
			pid, have = n, true
			if _, ok := listeners[pid]; !ok {
				listeners[pid] = Listener{}
			}
		case 'c':
			if !have {
				continue
			}
			l := listeners[pid]
			l.Command = value
			listeners[pid] = l
		case 'n':
			if !have {
				continue
			}
			port, err := portFrom(value)
			if err != nil {
				continue
			}
			l := listeners[pid]
			l.Ports = appendPort(l.Ports, port)
			listeners[pid] = l
		}
	}
	return listeners
}

func appendPort(ports []int, port int) []int {
	for _, have := range ports {
		if have == port {
			return ports
		}
	}
	return append(ports, port)
}

func portFrom(addr string) (int, error) {
	i := strings.LastIndex(addr, ":")
	if i < 0 || i == len(addr)-1 {
		return 0, errors.New("no port")
	}
	return strconv.Atoi(addr[i+1:])
}

func parsePS(out []byte) map[int]Proc {
	procs := map[int]Proc{}
	for _, line := range strings.Split(string(out), "\n") {
		parts := fourFields(line)
		if parts == nil {
			continue
		}
		pid, err := strconv.Atoi(parts[0])
		if err != nil {
			continue
		}
		ppid, err := strconv.Atoi(parts[1])
		if err != nil {
			continue
		}
		up, ok := parseEtime(parts[2])
		if !ok {
			continue
		}
		procs[pid] = Proc{PPID: ppid, Name: parts[3], Uptime: up}
	}
	return procs
}

// fourFields splits pid, ppid and etime on whitespace and keeps the rest as
// the command name, which may itself contain spaces.
func fourFields(line string) []string {
	line = strings.TrimSpace(line)
	var out []string
	for i := 0; i < 3; i++ {
		line = strings.TrimLeftFunc(line, unicode.IsSpace)
		end := strings.IndexFunc(line, unicode.IsSpace)
		if end < 0 {
			return nil
		}
		out = append(out, line[:end])
		line = line[end:]
	}
	name := strings.TrimSpace(line)
	if name == "" {
		return nil
	}
	return append(out, name)
}

func parseEtime(s string) (time.Duration, bool) {
	days := 0
	clock := s
	if daysPart, rest, ok := strings.Cut(s, "-"); ok {
		n, err := strconv.Atoi(daysPart)
		if err != nil {
			return 0, false
		}
		days, clock = n, rest
	}
	parts := strings.Split(clock, ":")
	if len(parts) < 2 || len(parts) > 3 {
		return 0, false
	}
	nums := make([]int, len(parts))
	for i, part := range parts {
		n, err := strconv.Atoi(part)
		if err != nil {
			return 0, false
		}
		nums[i] = n
	}
	for len(nums) < 3 {
		nums = append([]int{0}, nums...)
	}
	return time.Duration(days)*24*time.Hour + time.Duration(nums[0])*time.Hour + time.Duration(nums[1])*time.Minute + time.Duration(nums[2])*time.Second, true
}

// tableRow is one listening socket from a Windows TCP owner table.
type tableRow struct {
	Port int
	PID  int
}

func ipv4Listeners(buf []byte) ([]tableRow, error) { return tableListeners(buf, 24, 8, 20) }

func ipv6Listeners(buf []byte) ([]tableRow, error) { return tableListeners(buf, 56, 20, 52) }

// tableListeners reads a MIB_TCPTABLE_OWNER_PID or MIB_TCP6TABLE_OWNER_PID.
// The port DWORD is network order in its low 16 bits.
func tableListeners(buf []byte, rowSize, portOff, pidOff int) ([]tableRow, error) {
	if len(buf) < 4 {
		return nil, errors.New("tcp table is too short")
	}
	n := int(binary.LittleEndian.Uint32(buf[:4]))
	if n < 0 || n > (len(buf)-4)/rowSize {
		return nil, errors.New("tcp table is too short")
	}
	rows := make([]tableRow, 0, n)
	for i := 0; i < n; i++ {
		row := buf[4+i*rowSize:]
		rows = append(rows, tableRow{
			Port: portOf(binary.LittleEndian.Uint32(row[portOff:])),
			PID:  int(binary.LittleEndian.Uint32(row[pidOff:])),
		})
	}
	return rows, nil
}

func portOf(dword uint32) int {
	v := uint16(dword)
	return int(v<<8 | v>>8)
}
