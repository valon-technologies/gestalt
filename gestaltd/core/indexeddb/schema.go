package indexeddb

type ObjectStoreSchema struct {
	Indexes []IndexSchema
	Columns []ColumnDef // SQL providers use for DDL; NoSQL providers may ignore
}

type IndexSchema struct {
	Name    string
	KeyPath []string
	Unique  bool
}

type ColumnDef struct {
	Name       string
	Type       ColumnType
	PrimaryKey bool
	NotNull    bool
	Unique     bool
}

type ColumnType int

const (
	TypeString ColumnType = iota
	TypeInt
	TypeFloat
	TypeBool
	TypeTime
	TypeBytes
	TypeJSON
)
