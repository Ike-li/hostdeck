package listenerview

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"net"
	"strconv"
	"strings"
	"unicode"
)

// commandOutcome is one finished command. lsof exits 1 when nothing listens.
type commandOutcome struct {
	Out  []byte
	Exit int
	Err  error
}

type readFailure struct{ en, zh string }

func (e *readFailure) Error() string { return e.en }

func readingError(err error) ([]byte, error) {
	var failure *readFailure
	if !errors.As(err, &failure) {
		failure = &readFailure{en: "Could not list listening ports.", zh: "读不到正在监听的端口。"}
	}
	return json.Marshal(struct {
		Error text `json:"error"`
	}{words(failure.en, failure.zh)})
}

func snapshotFrom(lsof, ps commandOutcome) ([]Socket, map[int]Proc, error) {
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
			return nil, procs, nil
		}
		return nil, nil, &readFailure{
			en: "Could not list listening ports (exit status " + strconv.Itoa(lsof.Exit) + ").",
			zh: "读不到正在监听的端口（退出码 " + strconv.Itoa(lsof.Exit) + "）。",
		}
	}
	return parseLsof(lsof.Out), procs, nil
}

func parseLsof(out []byte) []Socket {
	var sockets []Socket
	pid := 0
	command := ""
	have := false
	seen := map[string]bool{}
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
			pid, command, have = n, "", true
		case 'c':
			if have {
				command = value
			}
		case 'n':
			if !have {
				continue
			}
			addr, port, err := parseListenName(value)
			if err != nil || port <= 0 {
				continue
			}
			key := strconv.Itoa(pid) + "|" + addr + "|" + strconv.Itoa(port)
			if seen[key] {
				continue
			}
			seen[key] = true
			sockets = append(sockets, Socket{PID: pid, Port: port, Addr: addr, Command: command})
		}
	}
	return sockets
}

func parseListenName(name string) (string, int, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", 0, errors.New("empty")
	}
	host, portStr, err := net.SplitHostPort(name)
	if err != nil {
		// lsof writes *:8080, which SplitHostPort rejects.
		i := strings.LastIndex(name, ":")
		if i < 0 || i == len(name)-1 {
			return "", 0, err
		}
		host, portStr = name[:i], name[i+1:]
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return "", 0, err
	}
	return normalizeAddr(host), port, nil
}

func normalizeAddr(host string) string {
	host = strings.TrimSpace(host)
	switch host {
	case "", "*", "0.0.0.0":
		return "0.0.0.0"
	case "::", "[::]":
		return "::"
	case "::1", "[::1]":
		return "::1"
	}
	if strings.HasPrefix(host, "[") && strings.HasSuffix(host, "]") {
		return host[1 : len(host)-1]
	}
	return host
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
		procs[pid] = Proc{PPID: ppid, Name: parts[3]}
	}
	return procs
}

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

type tableRow struct {
	Addr string
	Port int
	PID  int
}

func ipv4Listeners(buf []byte) ([]tableRow, error) {
	return tableListeners(buf, 24, 4, 8, 20, false)
}

func ipv6Listeners(buf []byte) ([]tableRow, error) {
	return tableListeners(buf, 56, 0, 20, 52, true)
}

func tableListeners(buf []byte, rowSize, addrOff, portOff, pidOff int, v6 bool) ([]tableRow, error) {
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
		addr := ""
		if v6 {
			ip := net.IP(append([]byte(nil), row[addrOff:addrOff+16]...))
			addr = normalizeAddr(ip.String())
		} else {
			v := binary.LittleEndian.Uint32(row[addrOff:])
			addr = normalizeAddr(net.IPv4(byte(v), byte(v>>8), byte(v>>16), byte(v>>24)).String())
		}
		rows = append(rows, tableRow{
			Addr: addr,
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
