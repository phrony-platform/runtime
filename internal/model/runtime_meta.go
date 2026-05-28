package model

// RuntimeMeta stores small key/value metadata for the runtime database.
type RuntimeMeta struct {
	Key   string `db:"key"`
	Value string `db:"value"`
}

func (RuntimeMeta) TableName() string { return "runtime_meta" }
