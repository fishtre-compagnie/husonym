// Package schema describes the tables of the engine test bench independently of the
// database they are created in.
//
// A bench case declares its tables once; a renderer turns them into the DDL of one
// database (MySQL first, PostgreSQL and SQL Server later). Types are neutral kinds, with
// a native escape hatch for the types only one database has (ENUM, BIT, GEOMETRY…),
// which are exactly the ones the bench needs to exercise.
package schema

// Dialect names a database the bench can create tables in.
type Dialect string

const (
	MySQL Dialect = "mysql"
)

// Kind is a database-neutral column type.
type Kind int

const (
	KindInt32 Kind = iota
	KindInt64
	KindUint64
	KindDecimal
	KindFloat64
	KindBool
	KindVarchar
	KindText
	KindBinary // fixed length
	KindBlob
	KindDate
	KindDateTime
	KindJSON
	// KindNative is a type only some databases have, written as is for each of them.
	KindNative
)

// Type is a column type: a neutral kind and its parameters.
type Type struct {
	Kind Kind
	// Length is the length of a Varchar or Binary, and the fractional seconds precision
	// of a DateTime.
	Length int
	// Precision and Scale describe a Decimal.
	Precision, Scale int
	// Native gives the type as written in each database, for KindNative.
	Native map[Dialect]string
}

func Int32() Type                 { return Type{Kind: KindInt32} }
func Int64() Type                 { return Type{Kind: KindInt64} }
func Uint64() Type                { return Type{Kind: KindUint64} }
func Float64() Type               { return Type{Kind: KindFloat64} }
func Bool() Type                  { return Type{Kind: KindBool} }
func Text() Type                  { return Type{Kind: KindText} }
func Blob() Type                  { return Type{Kind: KindBlob} }
func Date() Type                  { return Type{Kind: KindDate} }
func JSON() Type                  { return Type{Kind: KindJSON} }
func Varchar(length int) Type     { return Type{Kind: KindVarchar, Length: length} }
func Binary(length int) Type      { return Type{Kind: KindBinary, Length: length} }
func DateTime(precision int) Type { return Type{Kind: KindDateTime, Length: precision} }
func Decimal(precision, scale int) Type {
	return Type{Kind: KindDecimal, Precision: precision, Scale: scale}
}

// Native is a type written as is, per database.
func Native(byDialect map[Dialect]string) Type {
	return Type{Kind: KindNative, Native: byDialect}
}

// IsBinary reports whether values of the type are raw bytes rather than text.
func (t Type) IsBinary() bool {
	return t.Kind == KindBinary || t.Kind == KindBlob
}

// Column is one column of a table.
type Column struct {
	Name     string
	Type     Type
	Nullable bool
	// Default is a SQL expression, written as is (DEFAULT 0, DEFAULT CURRENT_TIMESTAMP).
	Default string
	// AutoIncrement marks an identity column (AUTO_INCREMENT, IDENTITY, GENERATED … AS
	// IDENTITY).
	AutoIncrement bool
	// Collation is written as is; empty keeps the table default.
	Collation string
	// GeneratedAs is the expression of a generated column, stored or virtual. Such a
	// column is never written: the source loader skips it and seeds give it nil.
	GeneratedAs     string
	GeneratedStored bool
	// OnUpdate is written as is (ON UPDATE CURRENT_TIMESTAMP(6)).
	OnUpdate string
	// Invisible hides the column from SELECT * (MySQL 8.0.23).
	Invisible bool
	// RawBytes marks a native-typed column whose values are raw bytes (BIT, GEOMETRY),
	// compared byte for byte like a binary column.
	RawBytes bool
}

// IsBinary reports whether the column holds raw bytes.
func (c *Column) IsBinary() bool { return c.RawBytes || c.Type.IsBinary() }

// Index is a secondary index.
type Index struct {
	Name    string
	Columns []string
	Unique  bool
}

// ForeignKey references another table of the same case.
type ForeignKey struct {
	Name       string
	Columns    []string
	RefTable   string
	RefColumns []string
	// OnDelete is written as is (CASCADE, SET NULL); empty keeps the database default.
	OnDelete string
	// Virtual foreign keys are not declared in the database: the job receives them as
	// virtual foreign keys, the way users describe legacy relations.
	Virtual bool
}

// Table is one table of a bench case.
type Table struct {
	Name        string
	Columns     []Column
	PrimaryKey  []string
	Indexes     []Index
	ForeignKeys []ForeignKey
	// Options end the CREATE TABLE statement, written as is (PARTITION BY …).
	Options string
}

// WritableColumns returns the columns an INSERT can name: every column but the
// generated ones.
func (t *Table) WritableColumns() []int {
	indexes := make([]int, 0, len(t.Columns))
	for i := range t.Columns {
		if t.Columns[i].GeneratedAs == "" {
			indexes = append(indexes, i)
		}
	}
	return indexes
}

// Column returns the named column, or nil.
func (t *Table) Column(name string) *Column {
	for i := range t.Columns {
		if t.Columns[i].Name == name {
			return &t.Columns[i]
		}
	}
	return nil
}

// ColumnNames returns the column names in declaration order.
func (t *Table) ColumnNames() []string {
	names := make([]string, len(t.Columns))
	for i := range t.Columns {
		names[i] = t.Columns[i].Name
	}
	return names
}

// Renderer writes the DDL of one database.
type Renderer interface {
	Dialect() Dialect
	QuoteIdent(name string) string
	// CreateTable returns the statement creating the table, without its declared foreign
	// keys: they are added once every table of the case exists.
	CreateTable(database string, t *Table) (string, error)
	// AddForeignKeys returns the statements declaring the non-virtual foreign keys.
	AddForeignKeys(database string, t *Table) []string
}
