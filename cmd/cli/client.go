package main

import (
	"context"
	"fmt"
	"io"

	grpc_health_v1 "github.com/phrony-platform/runtime/gen/grpc/health/v1"
	runtimev1 "github.com/phrony-platform/runtime/gen/phrony/runtime/v1"
	"github.com/phrony-platform/runtime/internal/cliout"
	"github.com/phrony-platform/runtime/internal/common"
	"github.com/phrony-platform/runtime/internal/version"
	"github.com/spf13/cobra"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type runtimeClients struct {
	conn    *grpc.ClientConn
	runtime runtimev1.RuntimeClient
	health  grpc_health_v1.HealthClient
}

func dialRuntime(ctx context.Context, runtimeAddrFlag string) (*runtimeClients, error) {
	addr := common.ResolveRuntimeAddr(runtimeAddrFlag)
	conn, err := grpc.NewClient(addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, fmt.Errorf("connect to runtime at %s: %w", addr, err)
	}

	return &runtimeClients{
		conn:    conn,
		runtime: runtimev1.NewRuntimeClient(conn),
		health:  grpc_health_v1.NewHealthClient(conn),
	}, nil
}

func openRuntime(ctx context.Context, w io.Writer, runtimeAddrFlag string) (*runtimeClients, error) {
	clients, err := dialRuntime(ctx, runtimeAddrFlag)
	if err != nil {
		return nil, err
	}
	warnVersionMismatch(ctx, w, clients.runtime)
	return clients, nil
}

func warnVersionMismatch(ctx context.Context, w io.Writer, runtimeClient runtimev1.RuntimeClient) {
	resp, err := runtimeClient.GetVersion(ctx, &runtimev1.GetVersionRequest{})
	if err != nil {
		return
	}
	if version.SameRelease(version.CLIVersion, resp.GetVersion()) {
		return
	}
	_ = cliout.WriteVersionMismatchWarning(w, version.CLIVersion, resp.GetVersion())
}

func (c *runtimeClients) Close() error {
	if c == nil || c.conn == nil {
		return nil
	}
	return c.conn.Close()
}

// testWithRuntimeClientHook, when set by tests, replaces dialRuntime.
var testWithRuntimeClientHook func(fn func(runtimev1.RuntimeClient) error) error

// withRuntimeClient dials the runtime, invokes fn with the runtime client, and
// closes the connection afterwards.
func withRuntimeClient(cmd *cobra.Command, runtimeAddrFlag string, fn func(runtimev1.RuntimeClient) error) error {
	if testWithRuntimeClientHook != nil {
		return testWithRuntimeClientHook(fn)
	}
	clients, err := openRuntime(cmd.Context(), cmd.ErrOrStderr(), runtimeAddrFlag)
	if err != nil {
		return err
	}
	defer clients.Close()
	return fn(clients.runtime)
}
