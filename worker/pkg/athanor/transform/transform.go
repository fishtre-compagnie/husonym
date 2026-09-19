// Package transform defines the transformer interface of the next-generation
// anonymization engine (code name Athanor).
//
// MOVEMENT 1 of the rework (Strangler Fig approach): this is the target
// interface. A transformer is no longer pinned to "one value of one column"; it
// becomes a typed node with four possible SCOPES (RFC §11):
//
//	Value   T -> T'                      (hash, faker, mask…) — today's model
//	Row     Row -> Row                   (reads/writes several columns)
//	Table   stream of batches -> batches (shuffle, aggregate preservation)
//	Dataset access to the whole graph    (cross-table entity resolution)
//
// Nothing breaks: the existing Benthos transformers are wrapped at Value scope by
// benthos_adapter.go, leaving today's code untouched. Row scope and beyond are the
// NEW capabilities that the current model cannot express (see example_row.go).
package transform

import "context"

// Ctx porte le contexte d'exécution d'un transformer. Minimal au Mouvement 1 ;
// il accueillera ensuite la clé/graine de cohérence déterministe (RFC §8), l'accès
// aux statistiques du Catalog, etc.
type Ctx struct {
	Context context.Context
	// Seed : graine déterministe pour les transformers natifs (RFC §8). Le wrapper
	// Benthos ne l'utilise pas — les Opts Neosync portent déjà leur propre RNG seedé.
	Seed int64
}

// Background renvoie un Ctx neutre, pratique pour les tests et les appels simples.
func Background() Ctx { return Ctx{Context: context.Background()} }

// ValueTransformer — VALUE scope: turns one value into another.
// Every existing Neosync transformer satisfies this contract through the adapter.
type ValueTransformer interface {
	TransformValue(ctx Ctx, in any) (out any, err error)
}

// RowTransformer — portée ROW : lit et écrit plusieurs colonnes d'une même ligne.
// Reads()/Writes() déclarent les dépendances de colonnes ; le futur compilateur
// s'en sert pour ordonner l'exécution (arêtes READS/WRITES du graphe, RFC §6).
type RowTransformer interface {
	Reads() []string
	Writes() []string
	TransformRow(ctx Ctx, row Row) error
}

// TableTransformer — portée TABLE : opère sur un flux de lignes (shuffle,
// préservation d'agrégats…). Défini ici pour figer le contrat ; l'implémentation
// du flux batché arrive avec le runtime vectorisé (Mouvement 3).
type TableTransformer interface {
	TransformTable(ctx Ctx, rows RowStream) error
}

// Row abstrait l'accès aux colonnes d'une ligne, indépendamment de la
// représentation physique sous-jacente (map aujourd'hui, colonnes Arrow demain).
type Row interface {
	Get(col string) (any, bool)
	Set(col string, v any) error
	Str(col string) string
}

// RowStream : un flux de lignes consommable par un TableTransformer. Contrat
// minimal au Mouvement 1 ; s'étoffera avec le batching Arrow.
type RowStream interface {
	Next() (Row, bool)
}

// MapRow est une implémentation de Row adossée à une map — utile pour les tests,
// la portée Row en attendant le moteur colonnes, et le preview.
type MapRow map[string]any

func (r MapRow) Get(col string) (any, bool) { v, ok := r[col]; return v, ok }
func (r MapRow) Set(col string, v any) error {
	r[col] = v
	return nil
}
func (r MapRow) Str(col string) string {
	if v, ok := r[col].(string); ok {
		return v
	}
	return ""
}
