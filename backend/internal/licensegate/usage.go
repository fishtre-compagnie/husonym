// Package licensegate decides what a license has to include for a job to start.
package licensegate

import (
	"context"
	"fmt"
	"strings"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	job_util "github.com/fishtre-compagnie/husonym/internal/job"
	"github.com/fishtre-compagnie/husonym/internal/license"
)

// TransformerLookup gives the configuration of a user-defined transformer. It returns nil, nil
// for one that no longer exists.
type TransformerLookup func(ctx context.Context, id string) (*mgmtv1alpha1.TransformerConfig, error)

// JobFacts is what the computation needs to know about a job: its definition, and what only the
// database knows, whether it has an enabled hook.
type JobFacts struct {
	Job             *mgmtv1alpha1.Job
	HasEnabledHooks bool
}

// FeaturesUsedBy lists the licensed features a job's definition uses, in the order of
// license.AllFeatures, each at most once. It never reports scheduling: a manual run goes through
// the job's schedule too, so the two cannot be told apart when a run starts.
//
// A user-defined transformer the job references counts as custom_transformers; what it stores is
// looked up to tell whether it is a PII-text transformer. lookup may be nil when the caller knows
// there is nothing to resolve. A lookup that fails fails the call: a feature is never guessed.
func FeaturesUsedBy(ctx context.Context, facts JobFacts, lookup TransformerLookup) ([]license.Feature, error) {
	job := facts.Job
	used := map[license.Feature]bool{
		license.FeatureJobHooks:     facts.HasEnabledHooks,
		license.FeaturePiiDetection: job.GetJobType().GetPiiDetect() != nil,
		license.FeatureSubsetting:   usesSubsetting(job),
	}

	stored := map[string]*mgmtv1alpha1.TransformerConfig{}
	for _, config := range transformerConfigsOf(job) {
		if config.GetTransformPiiTextConfig() != nil {
			used[license.FeaturePiiText] = true
		}
		if config.GetTransformJavascriptConfig() != nil || config.GetGenerateJavascriptConfig() != nil {
			used[license.FeatureCustomTransformers] = true
		}
		for _, id := range job_util.UserDefinedTransformerIds(config) {
			used[license.FeatureCustomTransformers] = true
			if lookup == nil {
				continue
			}
			resolved, seen := stored[id]
			if !seen {
				var err error
				resolved, err = lookup(ctx, id)
				if err != nil {
					return nil, fmt.Errorf("unable to look up user-defined transformer %s: %w", id, err)
				}
				stored[id] = resolved
			}
			// A user-defined transformer is made from a system transformer, never from another
			// user-defined one, so one level is all there is to resolve.
			if resolved.GetTransformPiiTextConfig() != nil {
				used[license.FeaturePiiText] = true
			}
		}
	}

	var features []license.Feature
	for _, feature := range license.AllFeatures() {
		if used[feature] {
			features = append(features, feature)
		}
	}
	return features, nil
}

// MissingFeatures returns, in the same order, the used features the license does not include.
// Without a license, every one of them is missing.
func MissingFeatures(lic license.EEInterface, used []license.Feature) []license.Feature {
	var missing []license.Feature
	for _, feature := range used {
		if lic == nil || !lic.HasFeature(feature) {
			missing = append(missing, feature)
		}
	}
	return missing
}

// RefusalMessage tells which features a job uses that the license does not include.
func RefusalMessage(missing []license.Feature) string {
	names := make([]string, 0, len(missing))
	for _, feature := range missing {
		names = append(names, string(feature))
	}
	return "this job uses features the license does not include: " + strings.Join(names, ", ")
}

// transformerConfigsOf gathers the transformer configurations a job runs: its mappings', and the
// four defaults DynamoDB applies to the attributes that have no mapping.
func transformerConfigsOf(job *mgmtv1alpha1.Job) []*mgmtv1alpha1.TransformerConfig {
	var configs []*mgmtv1alpha1.TransformerConfig
	for _, mapping := range job.GetMappings() {
		configs = append(configs, mapping.GetTransformer().GetConfig())
	}
	unmapped := job.GetSource().GetOptions().GetDynamodb().GetUnmappedTransforms()
	for _, transformer := range []*mgmtv1alpha1.JobMappingTransformer{
		unmapped.GetB(), unmapped.GetBoolean(), unmapped.GetN(), unmapped.GetS(),
	} {
		configs = append(configs, transformer.GetConfig())
	}
	return configs
}

// usesSubsetting reports whether any table of a SQL or DynamoDB source has a WHERE clause. The
// foreign-key subset flag alone only follows references, it does not subset.
func usesSubsetting(job *mgmtv1alpha1.Job) bool {
	options := job.GetSource().GetOptions()
	var clauses []string
	for _, schema := range options.GetPostgres().GetSchemas() {
		for _, table := range schema.GetTables() {
			clauses = append(clauses, table.GetWhereClause())
		}
	}
	for _, schema := range options.GetMysql().GetSchemas() {
		for _, table := range schema.GetTables() {
			clauses = append(clauses, table.GetWhereClause())
		}
	}
	for _, schema := range options.GetMssql().GetSchemas() {
		for _, table := range schema.GetTables() {
			clauses = append(clauses, table.GetWhereClause())
		}
	}
	for _, table := range options.GetDynamodb().GetTables() {
		clauses = append(clauses, table.GetWhereClause())
	}
	for _, clause := range clauses {
		if strings.TrimSpace(clause) != "" {
			return true
		}
	}
	return false
}
