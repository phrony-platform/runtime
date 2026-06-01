package core

import (
	runtimev1 "github.com/phrony-platform/runtime/gen/phrony/runtime/v1"
	"github.com/phrony-platform/runtime/internal/tooldispatch"
)

func (s *runtimeServer) Work(stream runtimev1.Runtime_WorkServer) error {
	reg := s.toolRegistry
	if reg == nil {
		reg = tooldispatch.NewWorkerRegistry(tooldispatch.DefaultRegistryConfig())
	}
	return (&tooldispatch.WorkStream{Registry: reg}).ServeWork(stream)
}
