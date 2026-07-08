package recommendations

import (
	"testing"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/stretchr/testify/require"
)

func Test_Recommend(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		category   Category
		columnName string
		dataType   mgmtv1alpha1.TransformerDataType
		wantCase   any
	}{
		{
			name:       "contact email",
			category:   CategoryContact,
			columnName: "user_email",
			dataType:   mgmtv1alpha1.TransformerDataType_TRANSFORMER_DATA_TYPE_STRING,
			wantCase:   &mgmtv1alpha1.TransformerConfig_TransformEmailConfig{},
		},
		{
			name:       "contact email exact match",
			category:   CategoryContact,
			columnName: "email",
			dataType:   mgmtv1alpha1.TransformerDataType_TRANSFORMER_DATA_TYPE_STRING,
			wantCase:   &mgmtv1alpha1.TransformerConfig_TransformEmailConfig{},
		},
		{
			name:       "contact phone",
			category:   CategoryContact,
			columnName: "phone_number",
			dataType:   mgmtv1alpha1.TransformerDataType_TRANSFORMER_DATA_TYPE_STRING,
			wantCase:   &mgmtv1alpha1.TransformerConfig_TransformPhoneNumberConfig{},
		},
		{
			name:       "contact fallback to generic string",
			category:   CategoryContact,
			columnName: "contact_notes",
			dataType:   mgmtv1alpha1.TransformerDataType_TRANSFORMER_DATA_TYPE_STRING,
			wantCase:   &mgmtv1alpha1.TransformerConfig_TransformStringConfig{},
		},
		{
			name:       "personal first name",
			category:   CategoryPersonal,
			columnName: "first_name",
			dataType:   mgmtv1alpha1.TransformerDataType_TRANSFORMER_DATA_TYPE_STRING,
			wantCase:   &mgmtv1alpha1.TransformerConfig_GenerateFirstNameConfig{},
		},
		{
			name:       "personal first name multilingual",
			category:   CategoryPersonal,
			columnName: "prenom",
			dataType:   mgmtv1alpha1.TransformerDataType_TRANSFORMER_DATA_TYPE_STRING,
			wantCase:   &mgmtv1alpha1.TransformerConfig_GenerateFirstNameConfig{},
		},
		{
			name:       "personal last name",
			category:   CategoryPersonal,
			columnName: "last_name",
			dataType:   mgmtv1alpha1.TransformerDataType_TRANSFORMER_DATA_TYPE_STRING,
			wantCase:   &mgmtv1alpha1.TransformerConfig_GenerateLastNameConfig{},
		},
		{
			name:       "personal full name",
			category:   CategoryPersonal,
			columnName: "full_name",
			dataType:   mgmtv1alpha1.TransformerDataType_TRANSFORMER_DATA_TYPE_STRING,
			wantCase:   &mgmtv1alpha1.TransformerConfig_GenerateFullNameConfig{},
		},
		{
			name:       "personal city",
			category:   CategoryPersonal,
			columnName: "ville",
			dataType:   mgmtv1alpha1.TransformerDataType_TRANSFORMER_DATA_TYPE_STRING,
			wantCase:   &mgmtv1alpha1.TransformerConfig_GenerateCityConfig{},
		},
		{
			name:       "location zipcode",
			category:   CategoryLocation,
			columnName: "postal_code",
			dataType:   mgmtv1alpha1.TransformerDataType_TRANSFORMER_DATA_TYPE_STRING,
			wantCase:   &mgmtv1alpha1.TransformerConfig_GenerateZipcodeConfig{},
		},
		{
			name:       "location street",
			category:   CategoryLocation,
			columnName: "street_address",
			dataType:   mgmtv1alpha1.TransformerDataType_TRANSFORMER_DATA_TYPE_STRING,
			wantCase:   &mgmtv1alpha1.TransformerConfig_GenerateStreetAddressConfig{},
		},
		{
			name:       "national id string",
			category:   CategoryNationalId,
			columnName: "national_id",
			dataType:   mgmtv1alpha1.TransformerDataType_TRANSFORMER_DATA_TYPE_STRING,
			wantCase:   &mgmtv1alpha1.TransformerConfig_GenerateUuidConfig{},
		},
		{
			name:       "financial uuid",
			category:   CategoryFinancial,
			columnName: "account_ref",
			dataType:   mgmtv1alpha1.TransformerDataType_TRANSFORMER_DATA_TYPE_UUID,
			wantCase:   &mgmtv1alpha1.TransformerConfig_TransformUuidConfig{},
		},
		{
			name:       "authentication numeric falls back to transform string",
			category:   CategoryAuthentication,
			columnName: "auth_code",
			dataType:   mgmtv1alpha1.TransformerDataType_TRANSFORMER_DATA_TYPE_INT64,
			wantCase:   &mgmtv1alpha1.TransformerConfig_TransformStringConfig{},
		},
		{
			name:       "unknown category defaults to passthrough",
			category:   "",
			columnName: "some_column",
			dataType:   mgmtv1alpha1.TransformerDataType_TRANSFORMER_DATA_TYPE_STRING,
			wantCase:   &mgmtv1alpha1.TransformerConfig_PassthroughConfig{},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := Recommend(tt.category, tt.columnName, tt.dataType)
			require.NotNil(t, got.Config)
			require.NotEmpty(t, got.Rationale)
			require.IsType(t, tt.wantCase, got.Config.GetConfig())
		})
	}
}
