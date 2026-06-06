package core

import (
	"context"

	"github.com/phrony-platform/runtime/internal/model"
	"github.com/phrony-platform/runtime/internal/provider"
	"github.com/phrony-platform/runtime/internal/store"
)

// messages is the in-memory history snapshot before tool dispatch; conversation
// state is persisted via message.* events on the interactive turn path.

func (st *interactiveSessionState) persistBeforeToolDispatch(
	ctx context.Context,
	q *store.Queries,
	messages []provider.Message,
) error {
	if q == nil || st.sessionID == "" {
		return nil
	}
	_, err := q.UpdateSession(ctx, store.UpdateSessionParams{
		ID:     st.sessionID,
		Status: model.SessionStatusAwaitingTool,
	})
	return err
}
