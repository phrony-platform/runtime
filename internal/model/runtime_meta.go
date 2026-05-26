package model

// RuntimeMeta stores small key/value metadata for the runtime database.
type RuntimeMeta struct {
	Key   string `gorm:"primaryKey"`
	Value string `gorm:"not null"`
}

func (RuntimeMeta) TableName() string { return "runtime_meta" }
