package shared

import (
	"context"
	"fmt"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	javascript_userland "github.com/fishtre-compagnie/husonym/internal/javascript/userland"
	te "github.com/fishtre-compagnie/husonym/worker/pkg/benthos/transformer_executor"
)

// BenthosRuns tells whether Benthos can run the JavaScript rules of a job, user-defined
// transformers included. The deterministic functions offered to rules (pseudo.*) derive
// from the consistency scope of Athanor, which Benthos does not have: the run is stopped
// at its start, naming the column, rather than on its first row, which Benthos retries
// until its activity times out.
func BenthosRuns(ctx context.Context, job *mgmtv1alpha1.Job, resolver te.UserDefinedTransformerResolver) error {
	for _, mapping := range job.GetMappings() {
		cfg := mapping.GetTransformer().GetConfig()
		if udt := cfg.GetUserDefinedTransformerConfig(); udt != nil {
			resolved, err := resolver.GetUserDefinedTransformer(ctx, udt.GetId())
			if err != nil {
				return fmt.Errorf("resolving the transformer of %s.%s.%s: %w",
					mapping.GetSchema(), mapping.GetTable(), mapping.GetColumn(), err)
			}
			cfg = resolved
		}
		code := cfg.GetTransformJavascriptConfig().GetCode()
		if code == "" {
			code = cfg.GetGenerateJavascriptConfig().GetCode()
		}
		if code == "" {
			continue
		}
		analysis, err := javascript_userland.Analyze(code)
		if err != nil {
			continue // code that does not compile fails its table, as it always did
		}
		if analysis.UsesPseudo {
			return fmt.Errorf("benthos cannot run the pseudo functions of the rule of %s.%s.%s: run the job with athanor",
				mapping.GetSchema(), mapping.GetTable(), mapping.GetColumn())
		}
	}
	return nil
}
