package piidetect_table_activities

import (
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
)

// SamplingMode determines what column data (if any) is sent to the LLM stage.
type SamplingMode string

const (
	// SamplingModeUnspecified means the mode was not set; callers should fall back to legacy behavior.
	SamplingModeUnspecified SamplingMode = ""
	// SamplingModeNone sends only column names and types to the LLM.
	SamplingModeNone SamplingMode = "none"
	// SamplingModeProfile samples rows locally and sends only aggregated shape profiles to the LLM.
	SamplingModeProfile SamplingMode = "profile"
	// SamplingModeRaw sends raw sampled values to the LLM (explicit opt-in).
	SamplingModeRaw SamplingMode = "raw"
)

// ResolveSamplingMode resolves the job's DataSampling config into a concrete SamplingMode.
// If the enum mode is unspecified, it falls back to the legacy is_enabled boolean
// (true maps to RAW for backwards compatibility).
func ResolveSamplingMode(
	dataSampling *mgmtv1alpha1.JobTypeConfig_JobTypePiiDetect_DataSampling,
) SamplingMode {
	switch dataSampling.GetMode() {
	case mgmtv1alpha1.JobTypeConfig_JobTypePiiDetect_DataSampling_SAMPLING_MODE_NONE:
		return SamplingModeNone
	case mgmtv1alpha1.JobTypeConfig_JobTypePiiDetect_DataSampling_SAMPLING_MODE_PROFILE:
		return SamplingModeProfile
	case mgmtv1alpha1.JobTypeConfig_JobTypePiiDetect_DataSampling_SAMPLING_MODE_RAW:
		return SamplingModeRaw
	default:
		if dataSampling.GetIsEnabled() {
			return SamplingModeRaw
		}
		return SamplingModeNone
	}
}

// resolveEffectiveSamplingMode resolves the request-level sampling mode,
// falling back to the legacy ShouldSample boolean when unspecified.
func resolveEffectiveSamplingMode(mode SamplingMode, legacyShouldSample bool) SamplingMode {
	if mode != SamplingModeUnspecified {
		return mode
	}
	if legacyShouldSample {
		return SamplingModeRaw
	}
	return SamplingModeNone
}
