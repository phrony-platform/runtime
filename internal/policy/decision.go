package policy

// Decision is the outcome of pre-dispatch policy evaluation for one tool call.
type Decision int

const (
	// DecisionAllow proceeds to dispatch (subject to runtime integrity checks).
	DecisionAllow Decision = iota
	// DecisionDeny rejects the call; the model receives a tool error result.
	DecisionDeny
	// DecisionRequireApproval suspends the session until a human approves or denies.
	DecisionRequireApproval
)

// FailureRoute selects how a dispatch-layer error is surfaced to the agent loop.
type FailureRoute int

const (
	// RouteFail aborts the turn with the dispatch error.
	RouteFail FailureRoute = iota
	// RouteToolError records the failure as a tool_result error block and continues the loop.
	RouteToolError
	// RouteEscalateHITL suspends for human review (approval_required).
	RouteEscalateHITL
)
