package sqlio

// normalize.go — correspondance de types SGBD → Go canonique.
//
// Problème : selon le driver, un scan en *any renvoie des types hétérogènes.
// Notamment, une colonne texte revient en []byte avec certains drivers (MySQL) et
// en string avec d'autres (PostgreSQL). Sans normalisation, `fmt.Sprint([]byte("x"))`
// et `fmt.Sprint("x")` diffèrent → GRAINES DE COHÉRENCE DIFFÉRENTES → la cohérence
// inter-bases (M2) casserait silencieusement. La normalisation garantit que la
// même valeur logique, quel que soit le SGBD, devient la même valeur Go canonique.
//
// Types canoniques produits : string, int64, float64, bool, time.Time, []byte
// (binaire véritable), nil.

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

// Kind indique comment interpréter une colonne. Auto décide d'après la valeur.
type Kind int

const (
	Auto    Kind = iota // déduire du type dynamique reçu
	Text                // forcer le texte : []byte -> string
	Binary              // conserver []byte tel quel (vrai binaire : bytea/blob)
	Integer             // forcer un entier
	Float               // forcer un flottant
	Bool                // forcer un booléen
)

// Normalizer convertit les valeurs brutes des drivers en valeurs Go canoniques.
// Les surcharges par colonne priment sur le mode Auto.
type Normalizer struct {
	byColumn map[string]Kind
}

// NewNormalizer construit un normaliseur en mode Auto pour toutes les colonnes.
func NewNormalizer() *Normalizer { return &Normalizer{byColumn: map[string]Kind{}} }

// With fixe le Kind d'une colonne (surcharge du mode Auto). Chaînable.
func (n *Normalizer) With(column string, k Kind) *Normalizer {
	n.byColumn[column] = k
	return n
}

// binaryDatabaseTypes lists the database type names (as reported by
// sql.ColumnType.DatabaseTypeName for pgx, go-sql-driver/mysql and go-mssqldb) whose
// values are true binary. Their []byte must reach the destination untouched: turned
// into a string, they fail as invalid UTF-8 or get corrupted by escape parsing.
var binaryDatabaseTypes = map[string]bool{
	"BYTEA":      true, // PostgreSQL
	"BINARY":     true, // MySQL, SQL Server
	"VARBINARY":  true, // MySQL, SQL Server
	"TINYBLOB":   true, // MySQL
	"BLOB":       true, // MySQL (TEXT columns are reported as TEXT)
	"MEDIUMBLOB": true, // MySQL
	"LONGBLOB":   true, // MySQL
	"IMAGE":      true, // SQL Server
}

// NormalizerForColumnTypes builds a normalizer from the database type name of each
// column (same order as columns): binary types are declared Binary, everything else
// stays Auto.
func NormalizerForColumnTypes(columns, databaseTypeNames []string) *Normalizer {
	n := NewNormalizer()
	for i, col := range columns {
		if i < len(databaseTypeNames) && binaryDatabaseTypes[strings.ToUpper(databaseTypeNames[i])] {
			n.With(col, Binary)
		}
	}
	return n
}

// Normalize renvoie la valeur canonique de v pour la colonne donnée.
func (n *Normalizer) Normalize(column string, v any) (any, error) {
	kind := Auto
	if n != nil {
		if k, ok := n.byColumn[column]; ok {
			kind = k
		}
	}
	switch kind {
	case Binary:
		return v, nil // on ne touche pas au binaire véritable
	case Text:
		return toString(v)
	case Integer:
		return toInt64(v)
	case Float:
		return toFloat64(v)
	case Bool:
		return toBool(v)
	default:
		return auto(v)
	}
}

// auto : normalisation déduite du type dynamique. []byte est supposé TEXTE
// (cas courant) ; pour du binaire véritable, déclarer la colonne en Binary.
func auto(v any) (any, error) {
	switch x := v.(type) {
	case nil:
		return nil, nil
	case []byte:
		return string(x), nil
	case string:
		return x, nil
	case bool:
		return x, nil
	case int:
		return int64(x), nil
	case int8:
		return int64(x), nil
	case int16:
		return int64(x), nil
	case int32:
		return int64(x), nil
	case int64:
		return x, nil
	case uint:
		return unsignedToInt64(uint64(x))
	case uint8:
		return int64(x), nil
	case uint16:
		return int64(x), nil
	case uint32:
		return int64(x), nil
	case uint64:
		return unsignedToInt64(x)
	case float32:
		return float64(x), nil
	case float64:
		return x, nil
	case time.Time:
		return x, nil
	default:
		return v, nil // type inconnu : laissé tel quel plutôt que d'échouer
	}
}

// unsignedToInt64 canonicalizes an unsigned integer without corrupting the ones
// that do not fit in an int64. MySQL BIGINT UNSIGNED reaches 2^64-1: converting
// silently would yield a NEGATIVE value, hence a wrong consistency seed and a wrong
// write at the destination. Past MaxInt64 the value is preserved as is, exactly as
// `auto` already does for unknown types.
func unsignedToInt64(x uint64) (any, error) {
	if x > math.MaxInt64 {
		return x, nil
	}
	return int64(x), nil
}

func toString(v any) (any, error) {
	switch x := v.(type) {
	case nil:
		return nil, nil
	case []byte:
		return string(x), nil
	case string:
		return x, nil
	default:
		return fmt.Sprint(x), nil
	}
}

func toInt64(v any) (any, error) {
	switch x := v.(type) {
	case nil:
		return nil, nil
	case int64:
		return x, nil
	case int, int8, int16, int32, uint, uint8, uint16, uint32, uint64, float32, float64:
		nv, err := auto(x) // ramène vers int64/float64
		if err != nil {
			return nil, err
		}
		if f, ok := nv.(float64); ok {
			return int64(f), nil
		}
		if u, ok := nv.(uint64); ok {
			return nil, fmt.Errorf("normalize: %d dépasse les bornes d'un int64", u)
		}
		return nv, nil
	case []byte:
		return parseInt(string(x))
	case string:
		return parseInt(x)
	default:
		return nil, fmt.Errorf("normalize: impossible de convertir %T en int64", v)
	}
}

func toFloat64(v any) (any, error) {
	switch x := v.(type) {
	case nil:
		return nil, nil
	case float64:
		return x, nil
	case float32:
		return float64(x), nil
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		nv, _ := auto(x)
		if i, ok := nv.(int64); ok {
			return float64(i), nil
		}
		return nv, nil
	case []byte:
		return parseFloat(string(x))
	case string:
		return parseFloat(x)
	default:
		return nil, fmt.Errorf("normalize: impossible de convertir %T en float64", v)
	}
}

func toBool(v any) (any, error) {
	switch x := v.(type) {
	case nil:
		return nil, nil
	case bool:
		return x, nil
	case []byte:
		return strconv.ParseBool(string(x))
	case string:
		return strconv.ParseBool(x)
	default:
		return nil, fmt.Errorf("normalize: impossible de convertir %T en bool", v)
	}
}

func parseInt(s string) (any, error) {
	i, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("normalize: %q n'est pas un entier: %w", s, err)
	}
	return i, nil
}

func parseFloat(s string) (any, error) {
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return nil, fmt.Errorf("normalize: %q n'est pas un flottant: %w", s, err)
	}
	return f, nil
}
