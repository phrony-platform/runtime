package core

import (
	"context"

	"github.com/phrony-platform/runtime/internal/model"
	"github.com/phrony-platform/runtime/internal/provider"
	"github.com/phrony-platform/runtime/internal/store"
)

func (st *interactiveSessionState) persistBeforeToolDispatch(
	ctx context.Context,
	q *store.Queries,
	messages []provider.Message,
) error {
	if q == nil || st.sessionID == "" {
		return nil
	}
	historyJSON, err := encodeHistory(messages)
	if err != nil {
		return err
	}
	_, err = q.UpdateSession(ctx, store.UpdateSessionParams{
		ID:      st.sessionID,
		Status:  model.SessionStatusAwaitingTool,
		History: historyJSON,
	})
	return err
}
