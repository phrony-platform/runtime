package policy

import (
	"fmt"
	"strings"

	"github.com/phrony-platform/runtime/internal/manifest"
)

// EvaluationOutcome is the merged result of all applicable policies for one tool call.
type EvaluationOutcome struct {
	Decision    Decision
	DenyMessage string
	Approval    *ApprovalMatch
}

// ApprovalMatch carries portable approval metadata for a matched policy.
type ApprovalMatch struct {
	PolicyName   string
	AuthorityRef string
	Reason       string
	OnModify     string
	Route        string
	Runtime      map[string]any
}

func approvalFromPolicy(p manifest.PolicySpec) *ApprovalMatch {
	return &ApprovalMatch{
		PolicyName:   p.Name,
		AuthorityRef: p.AuthorityRef,
		Reason:       p.Reason,
		OnModify:     p.OnModify,
		Route:        routeFromRuntime(p.Runtime),
		Runtime:      p.Runtime,
	}
}

func routeFromRuntime(runtime map[string]any) string {
	if len(runtime) == 0 {
		return ""
	}
	for _, key := range []string{"phrony.com/approver_role", "approver_role"} {
		if v, ok := runtime[key]; ok {
			if s := strings.TrimSpace(fmt.Sprint(v)); s != "" {
				return s
			}
		}
	}
	return ""
}
