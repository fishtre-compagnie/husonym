package presidio

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"

	"github.com/moby/moby/api/types/container"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	tcexec "github.com/testcontainers/testcontainers-go/exec"
	"github.com/testcontainers/testcontainers-go/wait"
)

const (
	langFr = "fr"
	langEn = "en"

	entityPerson = "PERSON"
	entityEmail  = "EMAIL_ADDRESS"

	// The recognizer of the image that returns French persons, and the score it starts at.
	personRecognizer = "MappedLabelsNerRecognizer"
	personThreshold  = 0.8
	spacyRecognizer  = "SpacyRecognizer"

	scoreDelta = 1e-9
)

// declaredEntities is what GET /supportedentities returns, in both languages.
var declaredEntities = []string{
	"CREDIT_CARD", "CRYPTO", "DATE_TIME", "EMAIL_ADDRESS", "FR_NIR", "FR_PHONE_NUMBER",
	"FR_POSTAL_CODE", "FR_SIRET", "IBAN_CODE", "IP_ADDRESS", "LOCATION", "MAC_ADDRESS", "NRP",
	"PERSON", "PHONE_NUMBER", "URL",
}

// expected is a finding as the analyzer returns it: type, character positions, score.
type expected struct {
	entityType string
	start, end int
	score      float64
}

func requireFindings(t *testing.T, want []expected, got []finding) {
	t.Helper()
	require.Lenf(t, got, len(want), "findings: %+v", got)
	for i, w := range want {
		assert.Equalf(t, w.entityType, got[i].EntityType, "finding %d: %+v", i, got[i])
		assert.Equalf(t, w.start, got[i].Start, "finding %d: %+v", i, got[i])
		assert.Equalf(t, w.end, got[i].End, "finding %d: %+v", i, got[i])
		assert.InDeltaf(t, w.score, got[i].Score, scoreDelta, "finding %d: %+v", i, got[i])
	}
}

func ofType(findings []finding, entityType string) []finding {
	var kept []finding
	for _, f := range findings {
		if f.EntityType == entityType {
			kept = append(kept, f)
		}
	}
	return kept
}

// byModel tells whether a finding comes from one of the two recognizers that run a model.
func byModel(f finding) bool {
	return f.Explanation.Recognizer == spacyRecognizer || f.Explanation.Recognizer == personRecognizer
}

// fromPatterns keeps the findings of the pattern recognizers, fromModels the others.
func fromPatterns(findings []finding) []finding {
	var kept []finding
	for _, f := range findings {
		if !byModel(f) {
			kept = append(kept, f)
		}
	}
	return kept
}

func fromModels(findings []finding) []finding {
	var kept []finding
	for _, f := range findings {
		if byModel(f) {
			kept = append(kept, f)
		}
	}
	return kept
}

// requireDeclaredEntities checks that no finding has an entity type the analyzer does not
// declare: a label of the model that is not mapped must not come out.
func requireDeclaredEntities(t *testing.T, findings []finding) {
	t.Helper()
	for _, f := range findings {
		require.Containsf(t, declaredEntities, f.EntityType, "finding: %+v", f)
	}
}

// requirePerson checks that the persons found are exactly one, covering name, returned by the
// image's person recognizer with a score above its threshold.
func requirePerson(t *testing.T, text, name string, findings []finding) finding {
	t.Helper()
	persons := ofType(findings, entityPerson)
	require.Lenf(t, persons, 1, "persons: %+v", persons)
	person := persons[0]
	assert.Equal(t, name, person.text(text))
	assert.Equal(t, personRecognizer, person.Explanation.Recognizer)
	assert.Greater(t, person.Score, personThreshold)
	assert.Less(t, person.Score, 1.0)
	return person
}

func Test_Analyzer_Health(t *testing.T) {
	baseURL := startAnalyzer(t)

	get(t, baseURL+"/health")
}

func Test_Analyzer_SupportedEntities(t *testing.T) {
	baseURL := startAnalyzer(t)

	for _, language := range []string{langFr, langEn} {
		t.Run(language, func(t *testing.T) {
			require.Equal(t, declaredEntities, supportedEntities(t, baseURL, language))
		})
	}
}

func Test_Analyzer_French_Person(t *testing.T) {
	baseURL := startAnalyzer(t)
	text := "Bonjour, je suis Hélène Marchand."

	findings := analyze(t, baseURL, langFr, text)

	person := requirePerson(t, text, "Hélène Marchand", findings)
	require.Equal(t, 17, person.Start)
	require.Equal(t, 32, person.End)
}

func Test_Analyzer_French_ProductAndCompanyNames(t *testing.T) {
	baseURL := startAnalyzer(t)

	findings := analyze(t, baseURL, langFr, "Commande de Tondeuse Verdia livrée par Batiloire.")

	require.Empty(t, ofType(findings, entityPerson))
	requireDeclaredEntities(t, findings)
}

func Test_Analyzer_French_PersonCityAndCompany(t *testing.T) {
	baseURL := startAnalyzer(t)
	// The model labels a city and a company too; only the person is mapped.
	text := "Hélène Marchand travaille chez Batiloire à Besançon."

	findings := analyze(t, baseURL, langFr, text)

	requirePerson(t, text, "Hélène Marchand", findings)
	requireDeclaredEntities(t, findings)
}

func Test_Analyzer_English_Findings(t *testing.T) {
	baseURL := startAnalyzer(t)

	cases := []struct {
		name string
		text string
		want []expected
	}{
		{
			name: "person and location",
			text: "My name is Margaret Holloway and I live in Portland.",
			want: []expected{
				{"PERSON", 11, 28, 0.85},
				{"LOCATION", 43, 51, 0.85},
			},
		},
		{
			name: "person, phone and date",
			text: "Please call Daniel Whitford at 212-555-0147 before Friday.",
			want: []expected{
				{"PERSON", 12, 27, 0.85},
				{"PHONE_NUMBER", 31, 43, 0.4},
				{"DATE_TIME", 51, 57, 0.85},
			},
		},
		{
			name: "email and date",
			text: "The invoice was sent to oliver.brandt@example.com on March 3rd, 2021.",
			want: []expected{
				{"URL", 24, 33, 0.5},
				{"EMAIL_ADDRESS", 24, 49, 1.0},
				{"URL", 38, 49, 0.5},
				{"DATE_TIME", 53, 68, 0.85},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			findings := analyze(t, baseURL, langEn, tc.text)

			requireFindings(t, tc.want, findings)
			for _, person := range ofType(findings, entityPerson) {
				require.Equal(t, spacyRecognizer, person.Explanation.Recognizer)
			}
		})
	}
}

// The findings of the pattern recognizers are compared apart from those of the two recognizers
// that run a model: on these texts the models return nothing, but for the email address, which
// the French spaCy model labels as a location.
func Test_Analyzer_French_PatternRecognizers(t *testing.T) {
	baseURL := startAnalyzer(t)

	cases := []struct {
		name       string
		text       string
		want       []expected
		wantModels []expected
	}{
		{
			name: "NIR",
			text: "Numéro de sécurité sociale : 2 84 05 75 123 456 78.",
			want: []expected{{"FR_NIR", 29, 50, 0.6}},
		},
		{
			name: "phone",
			text: "Téléphone : 06 12 34 56 78.",
			want: []expected{
				{"FR_PHONE_NUMBER", 12, 26, 0.6},
				{"PHONE_NUMBER", 12, 26, 0.75},
			},
		},
		{
			name: "postal code",
			text: "Code postal : 44000.",
			want: []expected{{"FR_POSTAL_CODE", 14, 19, 0.65}},
		},
		{
			name: "SIRET",
			text: "SIRET : 73282932000074.",
			want: []expected{{"FR_SIRET", 8, 22, 0.75}},
		},
		{
			name: "IBAN",
			text: "IBAN : FR1420041010050500013M02606.",
			want: []expected{{"IBAN_CODE", 7, 34, 1.0}},
		},
		{
			name: "email",
			text: "Courriel : contact@exemple-boutique.fr.",
			want: []expected{
				{"EMAIL_ADDRESS", 11, 38, 1.0},
				{"URL", 19, 38, 0.5},
			},
			wantModels: []expected{{"LOCATION", 11, 38, 0.85}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			findings := analyze(t, baseURL, langFr, tc.text)

			requireFindings(t, tc.want, fromPatterns(findings))
			requireFindings(t, tc.wantModels, fromModels(findings))
		})
	}
}

func Test_Analyzer_French_CharacterPositions(t *testing.T) {
	baseURL := startAnalyzer(t)
	// Accents, a ligature, a typographic apostrophe and a no-break space: each is one character
	// and several bytes.
	text := "Le cœur de l’été : reçu de Maëlys Guével à Besançon, courriel maelys@exemple.fr."

	findings := analyze(t, baseURL, langFr, text)

	requireDeclaredEntities(t, findings)
	persons := ofType(findings, entityPerson)
	require.Lenf(t, persons, 1, "persons: %+v", persons)
	require.Equal(t, "Maëlys Guével", persons[0].text(text))
	require.Equal(t, 27, persons[0].Start)
	require.Equal(t, 40, persons[0].End)
	emails := ofType(findings, entityEmail)
	require.Lenf(t, emails, 1, "emails: %+v", emails)
	require.Equal(t, "maelys@exemple.fr", emails[0].text(text))
	require.Equal(t, 62, emails[0].Start)
	require.Equal(t, 79, emails[0].End)
}

func Test_Analyzer_French_LongText_PersonAtTheEnd(t *testing.T) {
	baseURL := startAnalyzer(t)
	sentences := []string{
		"Le colis a été déposé au guichet avant midi. ",
		"La facture sera envoyée dès réception du bon de commande. ",
		"Le technicien a remplacé le joint et vérifié la pression. ",
		"Aucune pièce n'était disponible au dépôt ce matin. ",
		"Le rendez-vous est reporté à la semaine prochaine. ",
	}
	var filler strings.Builder
	for i := 0; utf8.RuneCountInString(filler.String()) < 7000; i++ {
		filler.WriteString(sentences[i%len(sentences)])
	}
	text := filler.String() + "Le dossier a été validé par Corentin Delaunay."

	findings := analyze(t, baseURL, langFr, text)

	requirePerson(t, text, "Corentin Delaunay", findings)
}

func Test_Analyzer_French_TextWithoutWhitespace_PersonAtTheEnd(t *testing.T) {
	baseURL := startAnalyzer(t)
	tail := "signataire:Mathilde.Rousseau"
	var references strings.Builder
	for i := 1; references.Len() < 5000-len(tail); i++ {
		fmt.Fprintf(&references, "ref-%04d;", i)
	}
	text := references.String()[:5000-len(tail)] + tail
	require.Equal(t, -1, strings.IndexFunc(text, unicode.IsSpace))

	findings := analyze(t, baseURL, langFr, text)

	requirePerson(t, text, "Mathilde.Rousseau", findings)
}

func Test_Analyzer_French_NameOfSeveralWords_AroundAChunkBoundary(t *testing.T) {
	baseURL := startAnalyzer(t)
	// The only spaces of the text are those of the name: a chunk boundary falls inside it for
	// some of the offsets.
	name := "Corentin Le Guével"
	references := strings.Repeat("ref-0001;", 120)

	for _, offset := range []int{351, 369, 387, 390, 396, 405} {
		t.Run(fmt.Sprintf("name at %d", offset), func(t *testing.T) {
			text := references[:offset] + name + ";" + references[:600]

			findings := analyze(t, baseURL, langFr, text)

			person := requirePerson(t, text, name, findings)
			require.Equal(t, offset, person.Start)
		})
	}
}

func Test_Analyzer_French_CharactersOfSeveralTokens_PersonAtTheEnd(t *testing.T) {
	baseURL := startAnalyzer(t)
	// The model reads each of these characters as two tokens.
	text := strings.Repeat("½", 800) + " Signé par Mathilde Rousseau."

	findings := analyze(t, baseURL, langFr, text)

	requirePerson(t, text, "Mathilde Rousseau", findings)
}

func Test_Analyzer_French_RequestedEntities(t *testing.T) {
	baseURL := startAnalyzer(t)
	text := "Hélène Marchand a écrit depuis helene.marchand@exemple.fr hier."

	cases := []struct {
		entity string
		want   string
	}{
		{entity: entityPerson, want: "Hélène Marchand"},
		{entity: entityEmail, want: "helene.marchand@exemple.fr"},
	}
	for _, tc := range cases {
		t.Run(tc.entity, func(t *testing.T) {
			findings := analyze(t, baseURL, langFr, text, tc.entity)

			require.NotEmpty(t, findings)
			var texts []string
			for _, f := range findings {
				require.Equal(t, tc.entity, f.EntityType)
				texts = append(texts, f.text(text))
			}
			require.Contains(t, texts, tc.want)
		})
	}
}

func Test_Analyzer_WithoutNetwork_French_Person(t *testing.T) {
	startAnalyzer(t) // builds the image the container below runs
	ctx := context.Background()
	text := "Bonjour, je suis Hélène Marchand."
	local := "http://localhost:" + strings.TrimSuffix(analyzerPort, "/tcp")

	offline, err := testcontainers.Run(
		ctx,
		analyzerImageRepo+":"+analyzerImageTag,
		testcontainers.WithHostConfigModifier(func(hostConfig *container.HostConfig) {
			hostConfig.NetworkMode = "none"
		}),
		testcontainers.WithWaitStrategy(
			wait.ForExec([]string{"curl", "-fsS", local + "/health"}).
				WithStartupTimeout(analyzerStartupTimeout),
		),
	)
	testcontainers.CleanupContainer(t, offline)
	require.NoError(t, err)

	body, err := json.Marshal(analyzeRequest{Text: text, Language: langFr, ReturnDecisionProcess: true})
	require.NoError(t, err)
	exitCode, output, err := offline.Exec(ctx, []string{
		"curl", "-fsS", "-H", "Content-Type: application/json", "-d", string(body), local + "/analyze",
	}, tcexec.Multiplexed())
	require.NoError(t, err)
	response, err := io.ReadAll(output)
	require.NoError(t, err)
	require.Zerof(t, exitCode, "curl: %s", response)

	findings, err := decodeFindings(response)
	require.NoError(t, err)
	requirePerson(t, text, "Hélène Marchand", findings)
}
