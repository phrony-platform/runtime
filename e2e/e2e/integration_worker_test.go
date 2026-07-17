//go:build integration

package e2e_test

import (
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"testing"

	"github.com/phrony-platform/runtime/e2e/e2e/harness"
)

var (
	sharedWorker *harness.Worker
	teardownOnce sync.Once
)

func integrationSetup() {
	stopOnSignal()
	addr := harness.RuntimeAddr()
	if !harness.RuntimeHealthy(addr) {
		fmt.Fprintf(os.Stderr, "e2e: runtime not reachable at %s (integration tests will skip)\n", addr)
		return
	}
	fmt.Fprintf(os.Stderr, "e2e: runtime healthy at %s\n", addr)
	fmt.Fprintf(os.Stderr, "e2e: starting playground worker\n")
	w, err := harness.StartWorker()
	if err != nil {
		fmt.Fprintf(os.Stderr, "e2e: worker start failed: %v\n", err)
		return
	}
	sharedWorker = w
	fmt.Fprintf(os.Stderr, "e2e: worker started\n")
}

func stopOnSignal() {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-ch
		integrationTeardown()
		os.Exit(1)
	}()
}

func integrationTeardown() {
	teardownOnce.Do(func() {
		if sharedWorker != nil {
			sharedWorker.Stop()
			sharedWorker = nil
		}
	})
}

// Worker returns the shared worker started in TestMain.
func Worker() *harness.Worker {
	return sharedWorker
}

func restartWorker(t *testing.T) {
	t.Helper()
	if sharedWorker != nil {
		sharedWorker.Stop()
		sharedWorker = nil
	}
	harness.Action(t, "restart playground worker")
	w, err := harness.StartWorker()
	if err != nil {
		t.Fatalf("restart worker: %v", err)
	}
	sharedWorker = w
	harness.Result(t, "worker restarted")
}

func stopSharedWorker(t *testing.T) {
	t.Helper()
	if sharedWorker != nil {
		harness.Action(t, "stop shared e2e worker subprocess")
		sharedWorker.Stop()
		sharedWorker = nil
	}
}

func requireWorker(t *testing.T) *harness.Worker {
	t.Helper()
	if w := Worker(); w == nil {
		t.Fatal("worker not started; ensure playground worker can connect to runtime")
	}
	return Worker()
}
