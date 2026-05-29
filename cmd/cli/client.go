package main

import (
	"context"
	"fmt"

	grpc_health_v1 "github.com/phrony-platform/runtime/gen/grpc/health/v1"
	runtimev1 "github.com/phrony-platform/runtime/gen/phrony/runtime/v1"
	"github.com/phrony-platform/runtime/internal/common"
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

func (c *runtimeClients) Close() error {
	if c == nil || c.conn == nil {
		return nil
	}
	return c.conn.Close()
}

// withRuntimeClient dials the runtime, invokes fn with the runtime client, and
// closes the connection afterwards.
func withRuntimeClient(cmd *cobra.Command, runtimeAddrFlag string, fn func(runtimev1.RuntimeClient) error) error {
	clients, err := dialRuntime(cmd.Context(), runtimeAddrFlag)
	if err != nil {
		return err
	}
	defer clients.Close()
	return fn(clients.runtime)
}
