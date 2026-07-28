package husonymtypes

import (
	"encoding/json"
	"fmt"
)

type Version uint

const (
	V1            Version = iota + 1
	LatestVersion         = V1
)

const (
	HusonymArrayId    = "HUSONYM_ARRAY"
	HusonymBitsId     = "HUSONYM_BIT"
	HusonymBinaryId   = "HUSONYM_BINARY"
	HusonymDateTimeId = "HUSONYM_DATETIME"
	HusonymIntervalId = "HUSONYM_INTERVAL"
)

type HusonymAdapter interface {
	HusonymMetadataType
	// Pgx
	ScanPgx(value any) error
	ValuePgx() (any, error)
	// Json
	ScanJson(value any) error
	ValueJson() (any, error)
	// Mysql
	ScanMysql(value any) error
	ValueMysql() (any, error)
	// Mssql
	ScanMssql(value any) error
	ValueMssql() (any, error)
}

type HusonymPgxValuer interface {
	ValuePgx() (any, error)
}

type HusonymMysqlValuer interface {
	ValueMysql() (any, error)
}

type HusonymMssqlValuer interface {
	ValueMssql() (any, error)
}

type HusonymJsonValuer interface {
	ValueJson() (any, error)
}

type Husonym struct {
	Version Version `json:"version"`
	TypeId  string  `json:"type_id"`
}
type BaseType struct {
	Husonym Husonym `json:"_husonym"`
}

type HusonymMetadataType interface {
	setVersion(Version)
	GetVersion() Version
}

type HusonymTypeOption func(HusonymAdapter) error

func WithVersion(version Version) HusonymTypeOption {
	return func(t HusonymAdapter) error {
		if !IsValidVersion(version) {
			return fmt.Errorf("invalid Husonym Type version: %d", version)
		}
		if version == 0 {
			t.setVersion(LatestVersion)
			return nil
		}
		t.setVersion(version)
		return nil
	}
}

func applyOptions(t HusonymAdapter, opts ...HusonymTypeOption) error {
	for _, opt := range opts {
		if err := opt(t); err != nil {
			return err
		}
	}
	return nil
}

func IsValidVersion(ver Version) bool {
	return ver == V1 || ver == LatestVersion
}

type JsonScanner struct{}

func (js *JsonScanner) ScanJson(value, target any) error {
	switch v := value.(type) {
	case []byte:
		return json.Unmarshal(v, target)
	case string:
		return json.Unmarshal([]byte(v), target)
	default:
		return fmt.Errorf("unsupported scan type for Json: %T", value)
	}
}

func (js *JsonScanner) ValueJson(value any) (any, error) {
	return json.Marshal(value)
}
