package process

import (
	"os"
	"os/exec"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

type Tree struct {
	cmd     *exec.Cmd
	job     windows.Handle
	mu      sync.Mutex
	stopped bool
}

func jobName(token string) *uint16 {
	if token == "" {
		return nil
	}
	s, _ := windows.UTF16PtrFromString("Local\\hostdeck-task-" + token)
	return s
}
func NewTree(cmd *exec.Cmd, token string) (*Tree, error) {
	job, err := windows.CreateJobObject(nil, jobName(token))
	if err != nil {
		return nil, err
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{BasicLimitInformation: windows.JOBOBJECT_BASIC_LIMIT_INFORMATION{LimitFlags: windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE}}
	if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
		windows.CloseHandle(job)
		return nil, err
	}
	t := &Tree{cmd: cmd, job: job}
	cmd.Cancel = t.Stop
	return t, nil
}
func (t *Tree) Started() error {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION|windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(t.cmd.Process.Pid))
	if err != nil {
		return err
	}
	defer windows.CloseHandle(h)
	if err := windows.AssignProcessToJobObject(t.job, h); err != nil {
		var code uint32
		if e := windows.GetExitCodeProcess(h, &code); e == nil && code != uint32(windows.STATUS_PENDING) {
			return nil
		}
		return err
	}
	return nil
}
func (t *Tree) Stop() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.stopped {
		return nil
	}
	t.stopped = true
	err := windows.TerminateJobObject(t.job, 1)
	if t.cmd.Process != nil {
		t.cmd.Process.Kill()
	}
	return err
}
func (t *Tree) Close() { t.Stop(); windows.CloseHandle(t.job) }

var openJob = windows.NewLazySystemDLL("kernel32.dll").NewProc("OpenJobObjectW")

func openNamedJob(token string, access uintptr) (windows.Handle, error) {
	name := jobName(token)
	if name == nil {
		return 0, windows.ERROR_INVALID_NAME
	}
	h, _, e := openJob.Call(access, 0, uintptr(unsafe.Pointer(name)))
	if h == 0 {
		return 0, e
	}
	return windows.Handle(h), nil
}
func Gone(id Identity) bool {
	h, err := openNamedJob(id.Token, 4)
	if err == windows.ERROR_FILE_NOT_FOUND {
		return true
	}
	if err != nil {
		return false
	}
	defer windows.CloseHandle(h)
	var info struct {
		TotalUser, TotalKernel, PeriodUser, PeriodKernel int64
		Faults, Total, Active, Terminated                uint32
	}
	if err := windows.QueryInformationJobObject(h, 1, uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info)), nil); err != nil {
		return false
	}
	return info.Active == 0
}
func killWorkerTree(token string) {
	if h, e := openNamedJob(token, 8); e == nil {
		windows.TerminateJobObject(h, 1)
		windows.CloseHandle(h)
	}
	os.Exit(125)
}
