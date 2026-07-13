package piidetect_table_activities

import (
	"testing"

	"github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1/mgmtv1alpha1connect"
	"github.com/fishtre-compagnie/husonym/internal/connectiondata"
	"github.com/fishtre-compagnie/husonym/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/log"
	"go.temporal.io/sdk/testsuite"
)

func Test_loadDictionaries(t *testing.T) {
	dictionary, err := loadDictionaries(dictionaryFiles)
	require.NoError(t, err)
	assert.NotEmpty(t, dictionary)
}

func Test_normalizeColumnName(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{"snake_case", "date_naissance", []string{"date", "naissance"}},
		{"camelCase", "dateNaissance", []string{"date", "naissance"}},
		{"PascalCase", "DateNaissanceClient", []string{"date", "naissance", "client"}},
		{"kebab-case", "code-postal", []string{"code", "postal"}},
		{"single token", "iban", []string{"iban"}},
		{"uppercase", "IBAN", []string{"iban"}},
		{"dots and spaces", "user address.street", []string{"user", "address", "street"}},
		{"empty", "", []string{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, normalizeColumnName(tt.input))
		})
	}
}

func Test_lookupColumnInDictionary(t *testing.T) {
	tests := []struct {
		name             string
		column           string
		expectedCategory PiiCategory
		expectedToken    string
		expectMatch      bool
	}{
		// French
		{"fr prenom", "prenom", PiiCategoryPersonal, "prenom", true},
		{"fr date_naissance", "date_naissance", PiiCategoryPersonal, "date_naissance", true},
		{"fr num_secu", "num_secu", PiiCategoryNationalId, "num_secu", true},
		{"fr code_postal camel", "codePostal", PiiCategoryLocation, "code_postal", true},
		{"fr mot_de_passe", "mot_de_passe", PiiCategoryAuth, "mot_de_passe", true},
		// German
		{"de vorname", "vorname", PiiCategoryPersonal, "vorname", true},
		{"de geburtsdatum", "geburtsdatum", PiiCategoryPersonal, "geburtsdatum", true},
		{"de anschrift", "anschrift", PiiCategoryLocation, "anschrift", true},
		{"de plz", "plz", PiiCategoryLocation, "plz", true},
		// Spanish
		{"es fecha_nacimiento", "fecha_nacimiento", PiiCategoryPersonal, "fecha_nacimiento", true},
		{"es codigo_postal", "codigo_postal", PiiCategoryLocation, "codigo_postal", true},
		{"es dni", "dni", PiiCategoryNationalId, "dni", true},
		// Italian
		{"it codice_fiscale", "codice_fiscale", PiiCategoryNationalId, "codice_fiscale", true},
		{"it cellulare", "cellulare", PiiCategoryContact, "cellulare", true},
		// Dutch
		{"nl bsn", "bsn", PiiCategoryNationalId, "bsn", true},
		{"nl geboortedatum", "geboortedatum", PiiCategoryPersonal, "geboortedatum", true},
		// Portuguese
		{"pt data_nascimento", "data_nascimento", PiiCategoryPersonal, "data_nascimento", true},
		{"pt cpf", "cpf", PiiCategoryNationalId, "cpf", true},
		// Polish
		{"pl pesel", "pesel", PiiCategoryNationalId, "pesel", true},
		{"pl nazwisko", "nazwisko", PiiCategoryPersonal, "nazwisko", true},
		// English + shared
		{"en iban with prefix", "customer_iban", PiiCategoryFinancial, "iban", true},
		{"en first_name", "first_name", PiiCategoryPersonal, "first_name", true},
		// Bigram inside a longer name
		{"bigram in longer name", "client_date_naissance", PiiCategoryPersonal, "date_naissance", true},
		// Non-matches
		{"technical column", "created_at", "", "", false},
		{"substring is not a token", "denomination", "", "", false},
		{"id column", "id", "", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			match, ok := lookupColumnInDictionary(tt.column)
			assert.Equal(t, tt.expectMatch, ok)
			if tt.expectMatch {
				assert.Equal(t, tt.expectedCategory, match.Category)
				assert.Equal(t, tt.expectedToken, match.Token)
			}
		})
	}
}

func Test_DetectPiiDictionary_Success(t *testing.T) {
	testSuite := &testsuite.WorkflowTestSuite{}
	testSuite.SetLogger(log.NewStructuredLogger(testutil.GetConcurrentTestLogger(t)))
	env := testSuite.NewTestActivityEnvironment()

	mockConnClient := mgmtv1alpha1connect.NewMockConnectionServiceClient(t)
	mockOpenAIClient := NewMockOpenAiCompletionsClient(t)
	mockConnDataBuilder := connectiondata.NewMockConnectionDataBuilder(t)
	mockJobClient := mgmtv1alpha1connect.NewMockJobServiceClient(t)

	activities := New(mockConnClient, mockOpenAIClient, mockConnDataBuilder, mockJobClient, "")

	env.RegisterActivity(activities)

	val, err := env.ExecuteActivity(activities.DetectPiiDictionary, &DetectPiiDictionaryRequest{
		ColumnData: []*ColumnData{
			{Column: "geburtsdatum", DataType: "date", IsNullable: true},
			{Column: "created_at", DataType: "timestamp", IsNullable: false},
		},
	})
	require.NoError(t, err)
	res := &DetectPiiDictionaryResponse{}
	err = val.Get(res)
	require.NoError(t, err)

	require.Len(t, res.PiiColumns, 1)
	match, ok := res.PiiColumns["geburtsdatum"]
	require.True(t, ok)
	assert.Equal(t, PiiCategoryPersonal, match.Category)
	assert.Equal(t, "geburtsdatum", match.Token)
}
