package husonym_types

type KeyType int

const (
	StringSet KeyType = iota
	NumberSet
	ObjectID
	Decimal128
	Timestamp
	Binary
)
