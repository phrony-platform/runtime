//go:build integration

package harness

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"
)

// killProcessGroup stops the process group for cmd (requires Setpgid on Start).
func killProcessGroup(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	pgid, err := syscall.Getpgid(cmd.Process.Pid)
	if err == nil {
		_ = syscall.Kill(-pgid, syscall.SIGTERM)
	} else {
		_ = cmd.Process.Signal(syscall.SIGTERM)
	}
}

// Worker runs the playground tool worker subprocess.
type Worker struct {
	cmd    *exec.Cmd
	stdout *safeBuffer
}

type safeBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *safeBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *safeBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// StartWorker launches cmd/worker from the playground root.
func StartWorker() (*Worker, error) {
	return startWorker(nil)
}

// StartNoDispatchWorker runs a connected worker that declines process_payment without a receipt.
// Use for F1 instead of leaving the tool queue empty (which parks at awaiting_tool).
func StartNoDispatchWorker() (*Worker, error) {
	return startWorker([]string{
		"PLAYGROUND_WORKER_MODE=nodispatch",
		"WORKER_ID=e2e-nodispatch-worker",
	})
}

// StartIndeterminateWorker runs a worker that drops mid-invoke to trigger dispatch:indeterminate.
func StartIndeterminateWorker() (*Worker, error) {
	return startWorker([]string{
		"PLAYGROUND_WORKER_MODE=indeterminate",
		"WORKER_ID=e2e-indeterminate-worker",
	})
}

func startWorker(extraEnv []string) (*Worker, error) {
	cmd := exec.Command("go", "run", "./cmd/worker")
	cmd.Dir = PlaygroundRoot()
	cmd.Env = os.Environ()
	cmd.Env = append(cmd.Env, "PHRONY_RUNTIME_ADDR="+RuntimeAddr())
	cmd.Env = append(cmd.Env, extraEnv...)

	w := &Worker{stdout: &safeBuffer{}}
	cmd.Stdout = io.MultiWriter(os.Stdout, w.stdout)
	cmd.Stderr = os.Stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	w.cmd = cmd
	time.Sleep(300 * time.Millisecond)
	return w, nil
}

// Stop terminates the worker process.
func (w *Worker) Stop() {
	if w == nil || w.cmd == nil || w.cmd.Process == nil {
		return
	}
	killProcessGroup(w.cmd)
	done := make(chan struct{})
	go func() {
		_ = w.cmd.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		if pgid, err := syscall.Getpgid(w.cmd.Process.Pid); err == nil {
			_ = syscall.Kill(-pgid, syscall.SIGKILL)
		} else {
			_ = w.cmd.Process.Kill()
		}
		<-done
	}
	w.cmd = nil
}

// Output returns captured worker stdout.
func (w *Worker) Output() string {
	if w == nil || w.stdout == nil {
		return ""
	}
	return w.stdout.String()
}

// ContainsReceipt reports whether worker output includes a payment receipt line.
func (w *Worker) ContainsReceipt() bool {
	return bytes.Contains([]byte(w.Output()), []byte("payment processed:"))
}
