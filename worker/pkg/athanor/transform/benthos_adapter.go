package transform

// benthos_adapter.go — THE seam of Movement 1.
//
// Every existing Neosync transformer (generators, transforms, JS, PII,
// passthrough) converges on a single type in today's worker:
//
//	type TransformerExecutor struct {
//	    Opts   any
//	    Mutate func(value any, opts any) (any, error)
//	}
//
// So we do NOT wrap each transformer one by one: we wrap the executor. In a single
// pass the whole current catalog becomes usable behind the new ValueTransformer
// interface — without touching existing code, and fully reversibly.

import (
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	te "github.com/fishtre-compagnie/husonym/worker/pkg/benthos/transformer_executor"
)

// neosyncValueAdapter wraps a Neosync *TransformerExecutor behind Athanor's
// ValueTransformer interface.
type neosyncValueAdapter struct {
	exec *te.TransformerExecutor
}

// TransformValue délègue à la logique Neosync existante. Le Ctx d'Athanor n'est
// pas utilisé ici : les Opts Neosync portent déjà leur propre configuration
// (RNG seedé, bornes…). Il le sera pour les transformers natifs.
func (a *neosyncValueAdapter) TransformValue(_ Ctx, in any) (any, error) {
	return a.exec.Mutate(in, a.exec.Opts)
}

// WrapNeosyncConfig compile une configuration de transformer Neosync existante et
// l'expose comme ValueTransformer Athanor. Point d'entrée unique de la
// compatibilité descendante : le nouveau moteur peut exécuter n'importe quel
// transformer du fork actuel via cet adaptateur.
func WrapNeosyncConfig(
	cfg *mgmtv1alpha1.TransformerConfig,
	opts ...te.TransformerExecutorOption,
) (ValueTransformer, error) {
	exec, err := te.InitializeTransformerByConfigType(cfg, opts...)
	if err != nil {
		return nil, err
	}
	return &neosyncValueAdapter{exec: exec}, nil
}

// Vérification à la compilation : l'adaptateur satisfait bien le contrat.
var _ ValueTransformer = (*neosyncValueAdapter)(nil)
