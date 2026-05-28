package store

import (
	"context"
)

const getRuntimeMetaValue = `
SELECT value
FROM runtime_meta
WHERE key = $1
`

func (q *Queries) GetRuntimeMetaValue(ctx context.Context, key string) (string, error) {
	row := q.db.QueryRowContext(ctx, getRuntimeMetaValue, key)
	var value string
	err := row.Scan(&value)
	return value, err
}
