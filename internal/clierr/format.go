package clierr

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Format renders err for terminal output (plain text, no log prefixes).
func Format(err error) string {
	if err == nil {
		return ""
	}
	parts := parts(err)
	if len(parts) == 0 {
		return "Error: unknown error"
	}
	lines := []string{"Error: " + parts[0]}
	for _, part := range parts[1:] {
		lines = append(lines, "  "+part)
	}
	return strings.Join(lines, "\n")
}

// WrapRPC converts a gRPC error into a short, human-readable error for CLI commands.
func WrapRPC(action string, err error) error {
	st, ok := status.FromError(err)
	if !ok {
		return fmt.Errorf("%s: %w", action, err)
	}
	if st.Code() == codes.Unimplemented {
		return fmt.Errorf("%s: %s (not implemented on this runtime yet)", action, st.Message())
	}
	return fmt.Errorf("%s: %s (%s)", action, st.Message(), st.Code())
}

func parts(err error) []string {
	var segments []string
	for err != nil {
		if st, ok := status.FromError(err); ok {
			segments = append(segments, grpcPart(st))
			return segments
		}
		var pathErr *os.PathError
		if errors.As(err, &pathErr) {
			if err != pathErr {
				if prefix := prefixBeforeWrap(err, pathErr); prefix != "" {
					segments = append(segments, prefix)
				}
			}
			segments = append(segments, strings.TrimSpace(pathErr.Error()))
			return segments
		}
		next := errors.Unwrap(err)
		if next == nil {
			segments = append(segments, strings.TrimSpace(err.Error()))
			return segments
		}
		if prefix := prefixBeforeWrap(err, next); prefix != "" {
			segments = append(segments, prefix)
		}
		err = next
	}
	return segments
}

func prefixBeforeWrap(outer, inner error) string {
	outerMsg := strings.TrimSpace(outer.Error())
	innerMsg := strings.TrimSpace(inner.Error())
	if innerMsg != "" && strings.HasSuffix(outerMsg, ": "+innerMsg) {
		return strings.TrimSpace(strings.TrimSuffix(outerMsg, ": "+innerMsg))
	}
	if i := strings.Index(outerMsg, ": "); i >= 0 {
		return strings.TrimSpace(outerMsg[:i])
	}
	return outerMsg
}

func grpcPart(st *status.Status) string {
	if st.Code() == codes.Unimplemented {
		return fmt.Sprintf("%s (not implemented on this runtime yet)", st.Message())
	}
	return fmt.Sprintf("%s (%s)", st.Message(), st.Code())
}
