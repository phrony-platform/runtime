package store

import "context"

const deleteSessionToolInvocations = `
DELETE FROM tool_invocations WHERE session_id = $1
`

const deleteSessionApprovalVotes = `
DELETE FROM approval_votes
WHERE approval_id IN (SELECT id FROM approvals WHERE session_id = $1)
`

const deleteSessionApprovals = `
DELETE FROM approvals WHERE session_id = $1
`

// DeleteSessionProjections removes derived tool and approval rows for a session.
// The event log is untouched; callers replay events to rebuild projections.
func (q *Queries) DeleteSessionProjections(ctx context.Context, sessionID string) error {
	if _, err := q.db.ExecContext(ctx, deleteSessionApprovalVotes, sessionID); err != nil {
		return err
	}
	if _, err := q.db.ExecContext(ctx, deleteSessionApprovals, sessionID); err != nil {
		return err
	}
	_, err := q.db.ExecContext(ctx, deleteSessionToolInvocations, sessionID)
	return err
}
