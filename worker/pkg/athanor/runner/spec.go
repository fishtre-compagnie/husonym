// Package runner plugs the Athanor engine into the Neosync job format: it turns a
// job's mappings into an executable plan and anonymizes a table end to end
// (SQL read → engine → SQL write).
//
// It is the keystone of the integration, tying the existing configuration format
// (mgmtv1alpha1.JobMapping) to the new engine (engine + sqlio) while reusing the
// adapter for existing transformers (transform.WrapNeosyncConfig).
package runner

import (
	"context"
	"fmt"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/worker/pkg/athanor/consistency"
	"github.com/fishtre-compagnie/husonym/worker/pkg/athanor/engine"
	"github.com/fishtre-compagnie/husonym/worker/pkg/athanor/native"
	"github.com/fishtre-compagnie/husonym/worker/pkg/athanor/transform"
	te "github.com/fishtre-compagnie/husonym/worker/pkg/benthos/transformer_executor"
)

// SpecForTable traduit les mappings d'un job pour une table donnée en une
// engine.Spec, et renvoie la liste ordonnée des colonnes (le schéma du batch).
//
// Les colonnes en Passthrough ne reçoivent PAS de binding : elles sont recopiées
// telles quelles. Les transformers définis par l'utilisateur sont d'abord résolus
// vers leur configuration. Si un deriver de cohérence est fourni, les transformers
// reconnus (prénom, nom, ville…) sont routés vers un DictFaker DÉTERMINISTE
// (RFC §8) ; les transformers JavaScript de la table s'exécutent ensemble, ligne par
// ligne (voir javascript.go) ; les autres passent par l'adaptateur Benthos.
func SpecForTable(
	ctx context.Context,
	mappings []*mgmtv1alpha1.JobMapping,
	schema, table string,
	deriver *consistency.Deriver,
	env *TransformEnv,
) (cols []string, spec engine.Spec, err error) {
	if env == nil {
		env = &TransformEnv{}
	}
	var jsColumns []javascriptColumn
	for _, m := range mappings {
		if m.GetSchema() != schema || m.GetTable() != table {
			continue
		}
		col := m.GetColumn()

		jmt := m.GetTransformer()
		if jmt == nil {
			return nil, engine.Spec{}, fmt.Errorf("runner: colonne %q sans transformer", col)
		}
		cfg, err := resolveTransformerConfig(ctx, jmt.GetConfig(), env.Resolver)
		if err != nil {
			return nil, engine.Spec{}, fmt.Errorf("runner: colonne %q: %w", col, err)
		}
		if cfg.GetGenerateDefaultConfig() != nil {
			// Ni lue ni écrite : l'omettre de l'INSERT laisse la destination appliquer
			// la valeur par défaut de la colonne.
			continue
		}
		cols = append(cols, col)
		if cfg.GetPassthroughConfig() != nil {
			continue // colonne conservée : aucun binding nécessaire
		}
		if cfg.GetNullconfig() != nil {
			spec.Values = append(spec.Values, engine.ValueBinding{Column: col, T: native.Null{}})
			continue
		}
		if js, ok := javascriptColumnOf(col, cfg); ok {
			jsColumns = append(jsColumns, js)
			continue
		}

		// Cohérence déterministe (RFC §8) : chemin prioritaire pour les types
		// reconnus. À défaut, adaptateur Benthos aléatoire.
		if vt, ok := deterministicValueTransformer(deriver, cfg); ok {
			spec.Values = append(spec.Values, engine.ValueBinding{Column: col, T: vt})
			continue
		}

		vt, werr := transform.WrapNeosyncConfig(cfg, env.ExecOptions...)
		if werr != nil {
			return nil, engine.Spec{}, fmt.Errorf("runner: colonne %q: %w", col, werr)
		}
		spec.Values = append(spec.Values, engine.ValueBinding{Column: col, T: vt})
	}

	if len(cols) == 0 {
		return nil, engine.Spec{}, fmt.Errorf("runner: aucun mapping pour %s.%s", schema, table)
	}
	if len(jsColumns) > 0 {
		rows, jerr := newJavascriptRows(cols, jsColumns, env, deriver)
		if jerr != nil {
			return nil, engine.Spec{}, jerr
		}
		spec.Rows = append(spec.Rows, rows)
	}
	return cols, spec, nil
}

// resolveTransformerConfig replaces a user-defined transformer by the configuration it
// points to, like the Benthos builder does before building the pipeline.
func resolveTransformerConfig(
	ctx context.Context,
	cfg *mgmtv1alpha1.TransformerConfig,
	resolver te.UserDefinedTransformerResolver,
) (*mgmtv1alpha1.TransformerConfig, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config de transformer nil")
	}
	udt := cfg.GetUserDefinedTransformerConfig()
	if udt == nil {
		return cfg, nil
	}
	if resolver == nil {
		return nil, fmt.Errorf("transformer défini par l'utilisateur %q sans résolveur", udt.GetId())
	}
	resolved, err := resolver.GetUserDefinedTransformer(ctx, udt.GetId())
	if err != nil {
		return nil, fmt.Errorf("résolution du transformer défini par l'utilisateur %q: %w", udt.GetId(), err)
	}
	if resolved == nil || resolved.GetConfig() == nil {
		return nil, fmt.Errorf("transformer défini par l'utilisateur %q sans configuration", udt.GetId())
	}
	return resolved, nil
}
