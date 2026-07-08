package piidetect_table_activities

import (
	"testing"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/stretchr/testify/assert"
)

func Test_ResolveSamplingMode(t *testing.T) {
	tests := []struct {
		name     string
		input    *mgmtv1alpha1.JobTypeConfig_JobTypePiiDetect_DataSampling
		expected SamplingMode
	}{
		{
			name:     "nil config defaults to none",
			input:    nil,
			expected: SamplingModeNone,
		},
		{
			name:     "unspecified mode with legacy enabled maps to raw",
			input:    &mgmtv1alpha1.JobTypeConfig_JobTypePiiDetect_DataSampling{IsEnabled: true},
			expected: SamplingModeRaw,
		},
		{
			name:     "unspecified mode with legacy disabled maps to none",
			input:    &mgmtv1alpha1.JobTypeConfig_JobTypePiiDetect_DataSampling{IsEnabled: false},
			expected: SamplingModeNone,
		},
		{
			name: "explicit none wins over legacy enabled",
			input: &mgmtv1alpha1.JobTypeConfig_JobTypePiiDetect_DataSampling{
				IsEnabled: true,
				Mode:      mgmtv1alpha1.JobTypeConfig_JobTypePiiDetect_DataSampling_SAMPLING_MODE_NONE,
			},
			expected: SamplingModeNone,
		},
		{
			name: "explicit profile",
			input: &mgmtv1alpha1.JobTypeConfig_JobTypePiiDetect_DataSampling{
				Mode: mgmtv1alpha1.JobTypeConfig_JobTypePiiDetect_DataSampling_SAMPLING_MODE_PROFILE,
			},
			expected: SamplingModeProfile,
		},
		{
			name: "explicit raw",
			input: &mgmtv1alpha1.JobTypeConfig_JobTypePiiDetect_DataSampling{
				Mode: mgmtv1alpha1.JobTypeConfig_JobTypePiiDetect_DataSampling_SAMPLING_MODE_RAW,
			},
			expected: SamplingModeRaw,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, ResolveSamplingMode(tt.input))
		})
	}
}

func Test_resolveEffectiveSamplingMode(t *testing.T) {
	tests := []struct {
		name         string
		mode         SamplingMode
		legacySample bool
		expected     SamplingMode
	}{
		{"explicit mode wins", SamplingModeProfile, true, SamplingModeProfile},
		{"unspecified with legacy sample maps to raw", SamplingModeUnspecified, true, SamplingModeRaw},
		{"unspecified without legacy sample maps to none", SamplingModeUnspecified, false, SamplingModeNone},
		{"explicit none wins over legacy sample", SamplingModeNone, true, SamplingModeNone},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, resolveEffectiveSamplingMode(tt.mode, tt.legacySample))
		})
	}
}
