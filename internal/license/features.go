package license

import "slices"

// Feature names an optional capability a license key can allow.
type Feature string

const (
	FeatureJobHooks           Feature = "job_hooks"
	FeatureAccountHooks       Feature = "account_hooks"
	FeaturePiiText            Feature = "pii_text"
	FeaturePiiDetection       Feature = "pii_detection"
	FeatureCustomTransformers Feature = "custom_transformers"
	FeatureSubsetting         Feature = "subsetting"
	FeatureScheduling         Feature = "scheduling"
	FeatureMappingReview      Feature = "mapping_review"
	FeatureApiKeys            Feature = "api_keys"
	FeatureMcp                Feature = "mcp"
	FeatureRbac               Feature = "rbac"
	FeatureSso                Feature = "sso"
	FeatureRunLogs            Feature = "run_logs"
)

// FeatureWildcard, as an entry of a key's feature list, allows every feature.
const FeatureWildcard = "*"

// AllFeatures returns every declared feature, in a stable order. The slice is the caller's.
func AllFeatures() []Feature {
	return []Feature{
		FeatureJobHooks,
		FeatureAccountHooks,
		FeaturePiiText,
		FeaturePiiDetection,
		FeatureCustomTransformers,
		FeatureSubsetting,
		FeatureScheduling,
		FeatureMappingReview,
		FeatureApiKeys,
		FeatureMcp,
		FeatureRbac,
		FeatureSso,
		FeatureRunLogs,
	}
}

// NotIncludedMessage is what a refusal says for a feature that a license in force does not
// include. It names the feature and nothing else: never a plan, never the key.
func NotIncludedMessage(f Feature) string {
	return "this license does not include " + string(f)
}

// NotInForceMessage is what a refusal says when no license is in force: there is none, or it
// is past its grace period.
const NotInForceMessage = "account does not have an active license"

// FeatureRefusal gives the reason the license refuses the feature, or nothing when it allows
// it. A license that is not in force is said first: it includes no feature, and "does not
// include" would then name the wrong cause.
func FeatureRefusal(lic EEInterface, f Feature) string {
	switch {
	case lic == nil || !lic.IsValid():
		return NotInForceMessage
	case !lic.HasFeature(f):
		return NotIncludedMessage(f)
	default:
		return ""
	}
}

// ParseFeature returns the feature a name designates. The wildcard is not a feature.
func ParseFeature(name string) (Feature, bool) {
	f := Feature(name)
	return f, slices.Contains(AllFeatures(), f)
}

// HasFeature reports whether the key's content allows f. It does not look at dates: whether the
// key is in force is the provider's concern. A key that does not list features, issued before the
// list existed, allows everything; a key that lists none allows no optional feature.
func (k *Key) HasFeature(f Feature) bool {
	return k.AllowsEveryFeature() || slices.Contains(k.Features, string(f))
}

// AllowsEveryFeature reports whether the key's content allows every feature, the ones a later
// version adds included: it lists none, or lists the wildcard.
func (k *Key) AllowsEveryFeature() bool {
	return k.Features == nil || slices.Contains(k.Features, FeatureWildcard)
}

// TelemetryMode is what a key asks of the instance about reporting its usage.
type TelemetryMode string

const (
	TelemetryOnline        TelemetryMode = "online"
	TelemetryOfflineReport TelemetryMode = "offline_report"
	TelemetryNone          TelemetryMode = "none"
)

// TelemetryMode returns the mode the key carries. A key that does not say, or says something
// this binary does not know, is online.
func (k *Key) TelemetryMode() TelemetryMode {
	switch m := TelemetryMode(k.Telemetry); m {
	case TelemetryOfflineReport, TelemetryNone:
		return m
	default:
		return TelemetryOnline
	}
}
