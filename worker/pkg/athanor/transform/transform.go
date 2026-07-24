// Package transform définit l'interface de transformer de la prochaine génération
// du moteur d'anonymisation (nom de code Athanor).
//
// MOUVEMENT 1 de la refonte (stratégie Strangler Fig) : on pose ici l'interface
// cible — un transformer n'est plus figé à « une valeur d'une colonne », mais
// devient un nœud typé avec quatre PORTÉES possibles (RFC §11) :
//
//	Value   T -> T'                     (hash, faker, masque…) — le modèle actuel
//	Row     Row -> Row                  (lit/écrit plusieurs colonnes)
//	Table   flux de batches -> batches  (shuffle, préservation d'agrégats)
//	Dataset accès au graphe entier      (résolution d'entités inter-tables)
//
// Rien n'est cassé : les transformers Benthos existants sont enveloppés en portée
// Value via benthos_adapter.go, sans modification du code actuel. Les portées Row
// et suivantes sont les capacités NOUVELLES que le modèle actuel ne peut pas
// exprimer (cf. example_row.go).
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

// ValueTransformer — portée VALUE : transforme une valeur en une autre.
// C'est le contrat que tout transformer Neosync existant satisfait via l'adaptateur.
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
