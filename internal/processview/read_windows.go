package processview

import (
	"context"
	"errors"
	"time"
	"unsafe"

	"github.com/Ike-li/hostdeck/internal/run"
	"golang.org/x/sys/windows"
)

var getProcessMemoryInfo = windows.NewLazySystemDLL("kernel32.dll").NewProc("K32GetProcessMemoryInfo")

type memoryCounters struct {
	Size, PageFaultCount                               uint32
	PeakWorkingSetSize, WorkingSetSize                 uintptr
	QuotaPeakPagedPoolUsage, QuotaPagedPoolUsage       uintptr
	QuotaPeakNonPagedPoolUsage, QuotaNonPagedPoolUsage uintptr
	PagefileUsage, PeakPagefileUsage                   uintptr
}

func readPlatform(ctx context.Context, _ string, _ run.Runner, metrics bool) ([]proc, error) {
	return readWindows(ctx, metrics, windowsSnapshot, func(ctx context.Context) error {
		timer := time.NewTimer(250 * time.Millisecond)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
			return nil
		}
	})
}

func windowsSnapshot(ctx context.Context, metrics bool) ([]sample, error) {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil, err
	}
	defer windows.CloseHandle(snap)
	var entry windows.ProcessEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))
	if err := windows.Process32First(snap, &entry); err != nil {
		return nil, err
	}
	var out []sample
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		p := sample{proc: proc{PID: int(entry.ProcessID), PPID: int(entry.ParentProcessID), Name: windows.UTF16ToString(entry.ExeFile[:])}}
		// PID 0 is the idle-time pseudo process, not an executing program.
		if p.PID != 0 {
			if metrics {
				readWindowsMetrics(&p)
			}
			out = append(out, p)
		}
		err := windows.Process32Next(snap, &entry)
		if errors.Is(err, windows.ERROR_NO_MORE_FILES) {
			break
		}
		if err != nil {
			return nil, err
		}
	}
	if len(out) == 0 {
		return nil, errors.New("empty process snapshot")
	}
	return out, nil
}

func readWindowsMetrics(p *sample) {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(p.PID))
	if err != nil {
		return
	}
	defer windows.CloseHandle(h)
	var creation, exit, kernel, user windows.Filetime
	if windows.GetProcessTimes(h, &creation, &exit, &kernel, &user) == nil {
		p.Started = filetimeTicks(creation)
		p.Ticks = filetimeTicks(kernel) + filetimeTicks(user)
		p.At = time.Now()
	}
	var memory memoryCounters
	memory.Size = uint32(unsafe.Sizeof(memory))
	if ok, _, _ := getProcessMemoryInfo.Call(uintptr(h), uintptr(unsafe.Pointer(&memory)), uintptr(memory.Size)); ok != 0 {
		bytes := uint64(memory.WorkingSetSize)
		p.Memory = &bytes
	}
	var token windows.Token
	if windows.OpenProcessToken(h, windows.TOKEN_QUERY, &token) == nil {
		defer token.Close()
		if user, err := token.GetTokenUser(); err == nil {
			p.User = user.User.Sid.String()
		}
	}
}

func filetimeTicks(t windows.Filetime) uint64 {
	return uint64(t.HighDateTime)<<32 | uint64(t.LowDateTime)
}
