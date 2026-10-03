package ddl

import (
	sqlmanager_shared "github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager/shared"
)

// Snapshot is what the catalog says of a selection, as read.
type Snapshot struct {
	Database Database
	// Tables holds the tables found, in request order, history tables included.
	Tables []*Table
	// Missing holds the requested tables the database does not have.
	Missing      []sqlmanager_shared.SchemaTable
	Sequences    []*Sequence
	Modules      []*Module
	Dependencies []*Dependency
	Notices      []*Notice
}

// Database is what sys.databases and the server say of the database read.
type Database struct {
	CompatibilityLevel int
	Collation          string
	MajorVersion       int
}

// Temporal types of sys.tables.
const (
	TemporalNone            = 0
	TemporalHistory         = 1
	TemporalSystemVersioned = 2
)

// Table is a row of sys.tables, with its period and the rows of its children.
type Table struct {
	ObjectID int64
	Schema   string
	Name     string

	TemporalType  int
	HistoryID     int64
	HistorySchema string
	HistoryName   string
	// RetentionPeriod is history_retention_period: -1 when the history is kept for ever.
	RetentionPeriod int
	// RetentionUnit is history_retention_period_unit_desc: DAY, WEEK, MONTH, YEAR or INFINITE.
	RetentionUnit     string
	PeriodStartColumn string
	PeriodEndColumn   string

	IsMemoryOptimized bool
	IsFileTable       bool
	IsExternal        bool
	IsNode            bool
	IsEdge            bool

	Columns     []*Column
	Indexes     []*Index
	ForeignKeys []*ForeignKey
	Checks      []*CheckConstraint
}

// Generated-always types of sys.columns. Those above GeneratedRowEnd belong to ledger tables.
const (
	GeneratedNot      = 0
	GeneratedRowStart = 1
	GeneratedRowEnd   = 2
)

// Column is a row of sys.columns with its declared type, the base of that type, and the rows of
// sys.default_constraints, sys.computed_columns, sys.identity_columns and sys.masked_columns
// that belong to it.
type Column struct {
	ColumnID int
	Name     string

	// TypeSchema and TypeName name the declared type: a system type or an alias.
	TypeSchema string
	TypeName   string
	// BaseTypeName names the system type the declared type is stored as: the type under an
	// alias, nvarchar for sysname, the declared type itself otherwise. Empty for a CLR type.
	BaseTypeName      string
	IsUserDefinedType bool
	IsAssemblyType    bool
	// TypeIsNullable, TypeHasRule and TypeHasDefault describe the alias type.
	TypeIsNullable bool
	TypeHasRule    bool
	TypeHasDefault bool

	// MaxLength is in bytes, -1 for max.
	MaxLength int
	Precision int
	Scale     int
	// Collation is empty for a column that has none.
	Collation string

	IsNullable   bool
	IsAnsiPadded bool
	IsRowGuidCol bool
	IsFilestream bool
	IsSparse     bool
	IsColumnSet  bool
	IsHidden     bool
	IsEncrypted  bool
	// HasXMLCollection tells an xml column bound to an XML schema collection.
	HasXMLCollection bool
	// HasRule tells a rule bound to the column.
	HasRule         bool
	GeneratedAlways int

	// HasDefault tells a default of any kind; DefaultName is empty when it is not a constraint
	// of the column but a stand-alone default bound to it.
	HasDefault        bool
	DefaultName       string
	DefaultDefinition string

	IsComputed         bool
	ComputedDefinition string
	IsPersisted        bool

	IsIdentity bool
	// IdentitySeed and IdentityIncrement are the values as text: they are of the column's type.
	IdentitySeed              string
	IdentityIncrement         string
	IdentityNotForReplication bool

	IsMasked        bool
	MaskingFunction string
}

// Index types of sys.indexes.
const (
	IndexClustered               = 1
	IndexNonClustered            = 2
	IndexXML                     = 3
	IndexSpatial                 = 4
	IndexClusteredColumnstore    = 5
	IndexNonClusteredColumnstore = 6
	IndexHash                    = 7
	IndexJSON                    = 9
)

// Index is a row of sys.indexes with its columns.
type Index struct {
	IndexID            int
	Name               string
	Type               int
	IsUnique           bool
	IsPrimaryKey       bool
	IsUniqueConstraint bool
	IsDisabled         bool
	IsPadded           bool
	IgnoreDupKey       bool
	AllowRowLocks      bool
	AllowPageLocks     bool
	// FillFactor is 0 for the server's default.
	FillFactor       int
	HasFilter        bool
	FilterDefinition string
	Columns          []*IndexColumn
}

// IndexColumn is a row of sys.index_columns, in index_column_id order.
type IndexColumn struct {
	Name string
	// KeyOrdinal is 0 for a column that is not part of the key.
	KeyOrdinal   int
	IsDescending bool
	IsIncluded   bool
}

// Referential actions of sys.foreign_keys.
const (
	ActionNone       = 0
	ActionCascade    = 1
	ActionSetNull    = 2
	ActionSetDefault = 3
)

// ForeignKey is a row of sys.foreign_keys with its columns.
type ForeignKey struct {
	Name                string
	ReferencedID        int64
	ReferencedSchema    string
	ReferencedTable     string
	DeleteAction        int
	UpdateAction        int
	IsDisabled          bool
	IsNotTrusted        bool
	IsNotForReplication bool
	Columns             []*ForeignKeyColumn
}

// ForeignKeyColumn is a row of sys.foreign_key_columns, in constraint_column_id order.
type ForeignKeyColumn struct {
	Name           string
	IsNullable     bool
	ReferencedName string
}

// CheckConstraint is a row of sys.check_constraints.
type CheckConstraint struct {
	Name                string
	Definition          string
	IsDisabled          bool
	IsNotTrusted        bool
	IsNotForReplication bool
}

// Sequence is a row of sys.sequences with its type.
type Sequence struct {
	ObjectID int64
	Schema   string
	Name     string

	TypeSchema        string
	TypeName          string
	BaseTypeName      string
	IsUserDefinedType bool
	TypeIsNullable    bool
	Precision         int
	Scale             int

	// The values are text: they are of the sequence's type.
	StartValue   string
	Increment    string
	MinimumValue string
	MaximumValue string
	CurrentValue string
	IsCycling    bool
	IsCached     bool
	// CacheSize is 0 when the catalog records none.
	CacheSize int
	// IsUsed tells a sequence that gave a value at least once.
	IsUsed bool
}

// Object types of sys.objects that the snapshot tells apart.
const (
	TypeTable          = "U"
	TypeView           = "V"
	TypeScalarFunction = "FN"
	TypeInlineFunction = "IF"
	TypeTableFunction  = "TF"
	TypeProcedure      = "P"
	TypeTrigger        = "TR"
	TypeSequence       = "SO"
	TypeSynonym        = "SN"
	TypeCheck          = "C"
	TypeDefault        = "D"
	TypeCLRScalar      = "FS"
	TypeCLRTableValued = "FT"
	TypeCLRProcedure   = "PC"
	TypeCLRTrigger     = "TA"
	TypeCLRAggregate   = "AF"
)

// Module is a view, a function, a procedure or a trigger: its row of sys.objects, and of
// sys.sql_modules when it has one.
type Module struct {
	ObjectID int64
	Schema   string
	Name     string
	// Type is the type code of sys.objects, with the two spaces of its padding removed.
	Type string
	// ParentID is the table or view of a trigger, 0 otherwise.
	ParentID             int64
	UsesAnsiNulls        bool
	UsesQuotedIdentifier bool
	IsSchemaBound        bool
	// HasDefinition tells a module whose text can be read: neither encrypted nor CLR.
	HasDefinition bool
	// IsDisabled tells a disabled trigger.
	IsDisabled bool
	// Definition is the text of sys.sql_modules, read only for the modules of the selection.
	Definition string
}

// Classes of sys.sql_expression_dependencies.
const (
	ClassObject = 1
	ClassType   = 6
)

// Dependency is a row of sys.sql_expression_dependencies.
type Dependency struct {
	// ReferencingID is the object that holds the expression: a module, a check or default
	// constraint, or the table of a computed column.
	ReferencingID   int64
	ReferencingType string
	// ReferencingParentID is the table of a referencing constraint, 0 otherwise.
	ReferencingParentID int64

	ReferencedClass int
	// ReferencedID is 0 for a reference the server did not resolve.
	ReferencedID int64
	// ReferencedType is the type code of a referenced object, empty for a type.
	ReferencedType string
	// ReferencedIsTableType and ReferencedIsAssemblyType describe a referenced type.
	ReferencedIsTableType    bool
	ReferencedIsAssemblyType bool
	// ReferencedSchema and ReferencedName are the catalog's names of what was resolved.
	ReferencedSchema string
	ReferencedName   string
}

// Kinds of notices: attributes of a table that the plan leaves out.
const (
	NoticeFilegroup          = "filegroup"
	NoticePartitioning       = "partitioning"
	NoticeCompression        = "compression"
	NoticeStatistics         = "statistics"
	NoticeExtendedProperties = "extended properties"
	NoticePermissions        = "permissions"
	NoticeFullTextIndex      = "full-text index"
	NoticeRowLevelSecurity   = "row-level security"
	NoticeChangeTracking     = "change tracking"
	NoticeChangeDataCapture  = "change data capture"
	NoticeTriggerOrder       = "trigger order"
	NoticeColumnstoreOrder   = "columnstore order"
)

// Notice is an attribute of a table that the plan leaves out: one per table and kind.
type Notice struct {
	ObjectID int64
	Kind     string
	// Detail names what the kind found: a filegroup, a compression, a trigger, an index.
	Detail string
	// Count is how many the kind found.
	Count int
}
