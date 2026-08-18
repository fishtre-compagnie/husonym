// Package runner intègre le moteur Athanor au format de job Neosync : il traduit
// les mappings d'un job en plan exécutable, et exécute l'anonymisation d'une
// table de bout en bout (lecture SQL → moteur → écriture SQL).
//
// C'est la clé de voûte de l'intégration : elle relie le format de configuration
// existant (mgmtv1alpha1.JobMapping) au nouveau moteur (engine + sqlio), en
// réutilisant l'adaptateur des transformers existants (transform.WrapNeosyncConfig).
package runner

import (
	"fmt"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/worker/pkg/athanor/consistency"
	"github.com/fishtre-compagnie/husonym/worker/pkg/athanor/engine"
	"github.com/fishtre-compagnie/husonym/worker/pkg/athanor/transform"
	te "github.com/fishtre-compagnie/husonym/worker/pkg/benthos/transformer_executor"
)

// SpecForTable traduit les mappings d'un job pour une table donnée en une
// engine.Spec, et renvoie la liste ordonnée des colonnes (le schéma du batch).
//
// Les colonnes en Passthrough ne reçoivent PAS de binding : elles sont recopiées
// telles quelles. Si un deriver de cohérence est fourni, les transformers
// reconnus (prénom, nom, ville…) sont routés vers un DictFaker DÉTERMINISTE
// (RFC §8) ; sinon on retombe sur l'adaptateur Benthos aléatoire. Les options
// te.TransformerExecutorOption (résolveur user-defined, PII text…) ne concernent
// que ce dernier chemin.
func SpecForTable(
	mappings []*mgmtv1alpha1.JobMapping,
	schema, table string,
	deriver *consistency.Deriver,
	opts ...te.TransformerExecutorOption,
) (cols []string, spec engine.Spec, err error) {
	for _, m := range mappings {
		if m.GetSchema() != schema || m.GetTable() != table {
			continue
		}
		col := m.GetColumn()
		cols = append(cols, col)

		jmt := m.GetTransformer()
		if jmt == nil {
			return nil, engine.Spec{}, fmt.Errorf("runner: colonne %q sans transformer", col)
		}
		cfg := jmt.GetConfig()
		if cfg == nil {
			return nil, engine.Spec{}, fmt.Errorf("runner: colonne %q: config de transformer nil", col)
		}
		if cfg.GetPassthroughConfig() != nil {
			continue // colonne conservée : aucun binding nécessaire
		}

		// Cohérence déterministe (RFC §8) : chemin prioritaire pour les types
		// reconnus. À défaut, adaptateur Benthos aléatoire.
		if vt, ok := deterministicValueTransformer(deriver, cfg); ok {
			spec.Values = append(spec.Values, engine.ValueBinding{Column: col, T: vt})
			continue
		}

		vt, werr := transform.WrapNeosyncConfig(cfg, opts...)
		if werr != nil {
			return nil, engine.Spec{}, fmt.Errorf("runner: colonne %q: %w", col, werr)
		}
		spec.Values = append(spec.Values, engine.ValueBinding{Column: col, T: vt})
	}

	if len(cols) == 0 {
		return nil, engine.Spec{}, fmt.Errorf("runner: aucun mapping pour %s.%s", schema, table)
	}
	return cols, spec, nil
}
