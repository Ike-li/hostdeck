package observation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/Ike-li/hostdeck/internal/atomicfile"
)

var ErrStartupUnknown = errors.New("service startup outcome is unknown")

// Launch exists only in the private control pipe and worker memory.
type Launch struct {
	Executable  string
	Arguments   []string
	CommandLine string
	Directory   string
	Environment []string
}
type Capture struct {
	Schema    int       `json:"schema"`
	ID        string    `json:"id"`
	Service   string    `json:"service"`
	Worker    Identity  `json:"worker"`
	Identity  Identity  `json:"identity"`
	State     string    `json:"state"`
	Started   time.Time `json:"started"`
	Updated   time.Time `json:"updated"`
	Completed time.Time `json:"completed,omitempty"`
	ExitCode  *int      `json:"exitCode,omitempty"`
	Signal    string    `json:"signal,omitempty"`
	Error     string    `json:"error,omitempty"`
}
type prepareRequest struct {
	Root   string
	ID     string
	Launch Launch
}
type preparedReply struct {
	Capture Capture
	Error   string
}
type Prepared struct {
	mu       sync.Mutex
	cmd      *exec.Cmd
	input    io.WriteCloser
	output   io.ReadCloser
	decoder  *json.Decoder
	done     chan error
	waitOnce sync.Once
	started  bool
	root, id string
}
type captureKey struct{}
type captureRequest struct{ root, id string }

func WithCapture(ctx context.Context, root, id string) context.Context {
	return context.WithValue(ctx, captureKey{}, captureRequest{root, id})
}
func PrepareFrom(ctx context.Context, launch Launch) (*Prepared, error) {
	request, ok := ctx.Value(captureKey{}).(captureRequest)
	if !ok {
		return nil, nil
	}
	return Prepare(ctx, request.root, request.id, launch)
}
func validateLaunch(spec Launch) error {
	if !filepath.IsAbs(spec.Executable) || !filepath.IsAbs(spec.Directory) {
		return errors.New("absolute program and directory required")
	}
	info, err := os.Stat(spec.Executable)
	if err != nil || !info.Mode().IsRegular() {
		return errors.New("program is not a regular file")
	}
	if err = executableAllowed(spec.Executable); err != nil {
		return err
	}
	info, err = os.Stat(spec.Directory)
	if err != nil || !info.IsDir() {
		return errors.New("working directory is unavailable")
	}
	if runtime.GOOS == "windows" {
		if len(spec.Arguments) == 0 && spec.CommandLine == "" {
			return errors.New("command line missing")
		}
	} else if len(spec.Arguments) == 0 {
		return errors.New("arguments missing")
	}
	for _, value := range append(append([]string{spec.CommandLine}, spec.Arguments...), spec.Environment...) {
		if strings.ContainsRune(value, 0) {
			return errors.New("launch value contains NUL")
		}
	}
	for _, value := range spec.Environment {
		key, _, ok := strings.Cut(value, "=")
		if runtime.GOOS == "windows" && key == "" && strings.HasPrefix(value, "=") {
			key, _, ok = strings.Cut(value[1:], "=")
			ok = ok && len(key) == 2 && key[1] == ':'
		}
		if !ok || key == "" {
			return errors.New("invalid environment entry")
		}
	}
	return nil
}
func serviceKey(spec Launch) string {
	data, _ := json.Marshal([]any{spec.Executable, spec.Arguments, spec.CommandLine, spec.Directory})
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
func Prepare(ctx context.Context, root, id string, spec Launch) (*Prepared, error) {
	if !validID(id) {
		return nil, ErrSession
	}
	if err := validateLaunch(spec); err != nil {
		return nil, err
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	if err = reserveCapture(root, id, serviceKey(spec)); err != nil {
		return nil, err
	}
	ready := false
	defer func() {
		if !ready {
			if record, err := ReadCapture(root, id); err == nil && record.State == "prepared" {
				record.State = "failed"
				record.Completed = time.Now().UTC()
				record.Updated = record.Completed
				_ = saveCapture(root, record)
			}
		}
	}()
	executable, err := os.Executable()
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(executable, "--internal-service-collector")
	detached(cmd, "")
	input, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	output, err := cmd.StdoutPipe()
	if err != nil {
		input.Close()
		return nil, err
	}
	cmd.Stderr = io.Discard
	if err = cmd.Start(); err != nil {
		input.Close()
		output.Close()
		return nil, err
	}
	p := &Prepared{cmd: cmd, input: input, output: output, decoder: json.NewDecoder(output), done: make(chan error, 1), root: root, id: id}

	bounded, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	result := make(chan error, 1)
	go func() {
		if err := json.NewEncoder(input).Encode(prepareRequest{root, id, spec}); err != nil {
			result <- err
			return
		}
		var reply preparedReply
		err := p.decoder.Decode(&reply)
		if err == nil && reply.Error != "" {
			err = errors.New(reply.Error)
		}
		result <- err
	}()
	select {
	case err = <-result:
	case <-bounded.Done():
		err = bounded.Err()
	}
	if err != nil {
		p.Close()
		return nil, fmt.Errorf("capture preparation: %w", err)
	}
	ready = true
	return p, nil
}
func (p *Prepared) Start() (Capture, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.started {
		return Capture{}, errors.New("collector already started")
	}
	if err := json.NewEncoder(p.input).Encode("start"); err != nil {
		return Capture{}, errors.Join(ErrStartupUnknown, err)
	}
	p.started = true
	done := make(chan preparedReply, 1)
	go func() {
		var reply preparedReply
		if err := p.decoder.Decode(&reply); err != nil {
			reply.Error = err.Error()
		}
		done <- reply
	}()
	select {
	case reply := <-done:
		if reply.Error != "" {
			if reply.Capture.State != "failed" {
				return reply.Capture, errors.Join(ErrStartupUnknown, errors.New(reply.Error))
			}
			return reply.Capture, errors.New(reply.Error)
		}
		return reply.Capture, nil
	case <-time.After(5 * time.Second):
		return Capture{}, ErrStartupUnknown
	}
}
func (p *Prepared) Close() {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.input.Close()
	p.output.Close()
	// Once authorized, losing the console must not terminate worker or service.
	if !p.started {
		p.cmd.Process.Kill()
	}
	p.waitOnce.Do(func() { go func() { p.done <- p.cmd.Wait() }() })
	if !p.started {
		select {
		case <-p.done:
		case <-time.After(time.Second):
		}
		if record, err := ReadCapture(p.root, p.id); err == nil {
			record.State = "abandoned"
			record.Completed = time.Now().UTC()
			record.Updated = record.Completed
			_ = saveCapture(p.root, record)
		}
	}
}
func ReadCapture(root, id string) (Capture, error) {
	if !validID(id) {
		return Capture{}, ErrSession
	}
	data, err := os.ReadFile(filepath.Join(root, id, "capture.json"))
	if err != nil {
		return Capture{}, err
	}
	if len(data) > 64<<10 {
		return Capture{}, errors.New("capture record too large")
	}
	var record Capture
	err = json.Unmarshal(data, &record)
	if record.ID != id || record.Schema != 1 || len(record.Service) != 64 {
		return Capture{}, ErrSession
	}
	switch record.State {
	case "prepared", "running", "exited", "failed", "abandoned":
	default:
		return Capture{}, errors.New("invalid capture state")
	}
	if (record.State == "exited" || record.State == "failed" || record.State == "abandoned") && record.Completed.IsZero() {
		return Capture{}, errors.New("missing capture completion")
	}
	if record.State == "running" && record.Completed.IsZero() && record.Identity.PID <= 1 {
		return Capture{}, errors.New("missing captured process")
	}
	return record, err
}
func saveCapture(root string, record Capture) error {
	data, err := json.Marshal(record)
	if err != nil {
		return err
	}
	return atomicfile.Write(filepath.Join(root, record.ID, "capture.json"), data)
}
func CaptureText(root, id string) (string, error) {
	if !validID(id) {
		return "", ErrSession
	}
	var body string
	for _, name := range []string{"previous.log", "output.log"} {
		f, err := openRegular(filepath.Join(root, id, name))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return "", err
		}
		info, err := f.Stat()
		if err != nil {
			f.Close()
			return "", err
		}
		b := make([]byte, min(int64(FragmentLimit), info.Size()))
		n, err := f.ReadAt(b, max(0, info.Size()-FragmentLimit))
		f.Close()
		if err != nil && err != io.EOF {
			return "", err
		}
		body = tail(body + string(b[:n]))
	}
	return body, nil
}

// Collector uses private pipes for a two-phase launch. After authorization it
// deliberately stops reading the parent pipe and outlives either console exit.
func Collector() int {
	if info, err := os.Stdin.Stat(); err != nil || info.Mode()&os.ModeNamedPipe == 0 {
		return 2
	}
	decoder := json.NewDecoder(io.LimitReader(os.Stdin, 2<<20))
	var request prepareRequest
	if decoder.Decode(&request) != nil || !validID(request.ID) || !filepath.IsAbs(request.Root) {
		return 2
	}
	reply := func(record Capture, err error) {
		out := preparedReply{Capture: record}
		if err != nil {
			out.Error = err.Error()
		}
		_ = json.NewEncoder(os.Stdout).Encode(out)
	}
	if err := validateLaunch(request.Launch); err != nil {
		reply(Capture{}, err)
		return 1
	}
	dir := filepath.Join(request.Root, request.ID)
	owner, err := os.OpenFile(filepath.Join(dir, "owner.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		reply(Capture{}, err)
		return 1
	}
	defer owner.Close()
	if err = storageLock(owner); err != nil {
		reply(Capture{}, err)
		return 1
	}
	record, err := ReadCapture(request.Root, request.ID)
	if err != nil {
		reply(Capture{}, err)
		return 1
	}
	writer, err := newCaptureWriter(dir)
	if err != nil {
		reply(record, err)
		return 1
	}
	defer writer.Close()
	record.Worker = ProcessIdentity(os.Getpid())
	record.Updated = time.Now().UTC()
	if err = saveCapture(request.Root, record); err != nil {
		reply(record, err)
		return 1
	}
	reply(record, nil)
	var permit string
	if decoder.Decode(&permit) != nil || permit != "start" {
		record.State = "abandoned"
		record.Completed = time.Now().UTC()
		saveCapture(request.Root, record)
		return 1
	}
	spec := request.Launch
	cmd := exec.Command(spec.Executable)
	if len(spec.Arguments) > 0 {
		cmd.Args = spec.Arguments
	}
	cmd.Dir = spec.Directory
	cmd.Env = spec.Environment
	if cmd.Env == nil {
		cmd.Env = []string{}
	}
	detached(cmd, spec.CommandLine)
	redacted := newCredentialWriter(writer, spec.Environment)
	cmd.Stdout = redacted
	cmd.Stderr = redacted
	cmd.WaitDelay = time.Second
	if err = cmd.Start(); err != nil {
		record.State = "failed"
		record.Error = err.Error()
		record.Completed = time.Now().UTC()
		record.Updated = record.Completed
		saveErr := saveCapture(request.Root, record)
		reply(record, errors.Join(err, saveErr))
		return 1
	}
	record.Identity = launchIdentity(cmd.Process.Pid)
	record.State = "running"
	record.Updated = time.Now().UTC()
	if record.Identity.Started.IsZero() {
		record.Identity.PID = cmd.Process.Pid
		record.Error = "could not verify service start identity"
	}
	err = saveCapture(request.Root, record)
	if err != nil {
		record.Error = err.Error()
	}
	if record.Error != "" && err == nil {
		err = ErrStartupUnknown
	}
	reply(record, err)
	// No launch information is ever serialized after this point.
	request.Launch = Launch{}
	spec = Launch{}
	waited := make(chan error, 1)
	go func() { waited <- cmd.Wait() }()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case err = <-waited:
			redacted.Flush()
			closeErr := writer.Close()
			record.State = "exited"
			record.Completed = time.Now().UTC()
			record.Updated = record.Completed
			code := cmd.ProcessState.ExitCode()
			record.ExitCode = &code
			record.Signal = exitSignal(cmd.ProcessState)
			if closeErr != nil {
				record.Error = closeErr.Error()
			}
			if err != nil && code < 0 && record.Signal == "" {
				record.Error = err.Error()
			}
			if saveCapture(request.Root, record) != nil {
				return 1
			}
			owner.Close()
			cleanupExisting(request.Root)
			return 0
		case <-ticker.C:
			record.Updated = time.Now().UTC()
			if err = writer.Err(); err != nil {
				record.Error = err.Error()
			}
			_ = saveCapture(request.Root, record)
		}
	}
}
