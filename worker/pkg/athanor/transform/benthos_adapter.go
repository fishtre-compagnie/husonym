package transform

// benthos_adapter.go — LA couture du Mouvement 1.
//
// Tous les transformers Neosync existants (générateurs, transforms, JS, PII,
// passthrough) convergent vers un seul type dans le worker actuel :
//
//	type TransformerExecutor struct {
//	    Opts   any
//	    Mutate func(value any, opts any) (any, error)
//	}
//
// On n'enveloppe donc PAS chaque transformer un par un : on enveloppe
// l'exécuteur. En une passe, tout le catalogue actuel devient utilisable derrière
// la nouvelle interface ValueTransformer — sans toucher au code existant, et de
// façon totalement réversible.

import (
	mgmtv1alpha1 "github.com/Groupe-Hevea/neosync/backend/gen/go/protos/mgmt/v1alpha1"
	te "github.com/Groupe-Hevea/neosync/worker/pkg/benthos/transformer_executor"
)

// neosyncValueAdapter enveloppe un *TransformerExecutor Neosync derrière
// l'interface ValueTransformer d'Athanor.
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
