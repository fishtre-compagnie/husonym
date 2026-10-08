package presidio

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strings"
	"testing"
	"time"
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
	personRecognizer = "OnnxNerRecognizer"
	personThreshold  = 0.6
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

// The image starts gunicorn with a worker timeout of its own, 120 seconds unless WORKER_TIMEOUT
// says otherwise: gunicorn's default of 30 seconds kills the worker in the middle of a long text.
func Test_Analyzer_Gunicorn_WorkerTimeout(t *testing.T) {
	startAnalyzer(t)

	// Process 1 is gunicorn's master: the image's command replaces its shell by it.
	exitCode, output, err := analyzer.TestContainer.Exec(
		t.Context(), []string{"cat", "/proc/1/cmdline"}, tcexec.Multiplexed(),
	)
	require.NoError(t, err)
	cmdline, err := io.ReadAll(output)
	require.NoError(t, err)
	require.Zerof(t, exitCode, "cat: %s", cmdline)

	arguments := strings.Split(strings.TrimRight(string(cmdline), "\x00"), "\x00")
	require.Truef(t, slices.ContainsFunc(arguments, func(argument string) bool {
		return strings.HasSuffix(argument, "gunicorn")
	}), "process 1 is not gunicorn: %q", arguments)
	option := slices.Index(arguments, "--timeout")
	require.NotEqualf(t, -1, option, "arguments: %q", arguments)
	require.Greaterf(t, len(arguments), option+1, "arguments: %q", arguments)
	require.Equal(t, "120", arguments[option+1])
}

// ONNX Runtime reports usage events to its publisher unless it is told not to, and then keeps an
// identifier and a queue of events in the user's directory. The image turns it off: after a
// French text went through the model, the server runs with the variable that does so, and that
// directory does not exist.
func Test_Analyzer_OnnxRuntime_ReportsNoUsage(t *testing.T) {
	baseURL := startAnalyzer(t)
	text := "Bonjour, je suis Hélène Marchand."
	requirePerson(t, text, "Hélène Marchand", analyze(t, baseURL, langFr, text))

	for _, command := range []string{
		`tr '\0' '\n' < /proc/1/environ | grep -qx 'ORT_DISABLE_TELEMETRY=1'`,
		`test ! -e "$HOME/.cache/Microsoft"`,
	} {
		exitCode, output, err := analyzer.TestContainer.Exec(
			t.Context(), []string{"sh", "-c", command}, tcexec.Multiplexed(),
		)
		require.NoError(t, err)
		message, err := io.ReadAll(output)
		require.NoError(t, err)
		require.Zerof(t, exitCode, "%s: %s", command, message)
	}
}

// A text of some fifty chunks returns its findings. A single observation on this image without a
// CPU quota took 1.3 s for these 20,000 characters of prose: long enough to be far from a short
// request, short enough to end well before any timeout on a slower machine. The duration is
// logged and not asserted.
func Test_Analyzer_French_TextOfManyChunks_PersonAtTheEnd(t *testing.T) {
	baseURL := startAnalyzer(t)
	sentences := []string{
		"Le colis a été déposé au guichet avant midi. ",
		"La facture sera envoyée dès réception du bon de commande. ",
		"Le technicien a remplacé le joint et vérifié la pression. ",
		"Aucune pièce n'était disponible au dépôt ce matin. ",
		"Le rendez-vous est reporté à la semaine prochaine. ",
	}
	tail := "Le dossier a été validé par Corentin Delaunay."
	var filler strings.Builder
	characters := 0
	for i := 0; characters < 20000-utf8.RuneCountInString(tail); i++ {
		filler.WriteString(sentences[i%len(sentences)])
		characters += utf8.RuneCountInString(sentences[i%len(sentences)])
	}
	text := filler.String() + tail

	start := time.Now()
	findings := analyze(t, baseURL, langFr, text)
	t.Logf("%d characters analyzed in %s", utf8.RuneCountInString(text), time.Since(start).Round(100*time.Millisecond))

	requirePerson(t, text, "Corentin Delaunay", findings)
	requireDeclaredEntities(t, findings)
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

// In a text with few or no spaces, a name of several words is not always returned whole: the
// model may return a part of it, or nothing, wherever the name stands, and a chunk boundary
// that falls inside the name adds to it. The README states it as a limit of the image. What is
// asserted is what holds at every position: the request succeeds, and a person finding only
// designates characters of the name or the two before it. How often each name comes back whole
// is counted, logged, and held to a floor a few positions under the count observed on this
// image: a name the image stops finding fails the test, and so does a filler it reads worse.
func Test_Analyzer_French_NamesOfSeveralWords_InTextWithFewSpaces(t *testing.T) {
	baseURL := startAnalyzer(t)

	const (
		references  = "references"
		compactJSON = "compact JSON"
		// A finding may take a character before the name with it (a quote, a hyphen).
		marginBefore = 2
		// The floor of a name is the count observed less this many positions.
		tolerance = 5
	)
	fillers := []struct {
		name          string
		filler        string
		before, after string
	}{
		{name: references, filler: "ref-0001;", before: "", after: ";"},
		{name: compactJSON, filler: `{"id":17,"ref":"A-0001"},`, before: `{"nom":"`, after: `"},`},
	}
	// The positions, of 60, where each name was returned whole on this image.
	persons := []struct {
		name     string
		observed map[string]int
	}{
		{name: "Corentin Le Guével", observed: map[string]int{references: 52, compactJSON: 38}},
		{name: "Mathilde Rousseau de Kerbrat", observed: map[string]int{references: 60, compactJSON: 57}},
		{name: "Anne-Sophie Marchand", observed: map[string]int{references: 37, compactJSON: 56}},
		// A name the model returns whole at no position of these texts, and in part at some.
		{name: "Corentin de La Brosse", observed: map[string]int{references: 0, compactJSON: 0}},
	}

	for _, tc := range fillers {
		filler := strings.Repeat(tc.filler, 120)
		for _, person := range persons {
			whole, positions := 0, 0
			// Every third position of a window wider than a chunk's end can move: the first
			// chunk ends before the name, on each of its spaces, or after it.
			for offset := 300; offset < 480; offset += 3 {
				prefix := filler[:offset-len(tc.before)] + tc.before
				text := prefix + person.name + tc.after + filler[:600]
				end := offset + utf8.RuneCountInString(person.name)

				findings := ofType(analyze(t, baseURL, langFr, text), entityPerson)

				positions++
				for _, f := range findings {
					require.Equal(t, personRecognizer, f.Explanation.Recognizer)
					require.GreaterOrEqualf(t, f.Start, offset-marginBefore, "%s, %s at %d: %+v", tc.name, person.name, offset, f)
					require.LessOrEqualf(t, f.End, end, "%s, %s at %d: %+v", tc.name, person.name, offset, f)
					if f.Start == offset && f.End == end {
						whole++
					}
				}
			}
			t.Logf("%s, %s: whole at %d positions of %d", tc.name, person.name, whole, positions)
			assert.GreaterOrEqualf(t, whole, person.observed[tc.name]-tolerance,
				"%s, %s: positions where the name is whole, of %d", tc.name, person.name, positions)
		}
	}
}

// Two chunks can each return a part of the passage where two persons follow each other, and
// findings of two chunks that overlap come back as one: the two names may be one finding or
// two. What is checked is that every character of both names is in a finding.
func Test_Analyzer_French_TwoPersonsSideBySide_AroundAChunkBoundary(t *testing.T) {
	baseURL := startAnalyzer(t)
	sentence := "Le colis a été déposé au guichet avant midi. "
	shifts := []string{"", "Vu. ", "Bien reçu. ", "Dossier complet. ", "Rien à signaler ce jour. "}
	first, second := "Corentin Delaunay", "Mathilde Rousseau"

	for _, shift := range shifts {
		prefix := strings.Repeat(sentence, 8) + shift + "Le dossier a été validé par "
		offset := utf8.RuneCountInString(prefix)
		t.Run(fmt.Sprintf("names at %d", offset), func(t *testing.T) {
			text := prefix + first + ", " + second + " hier soir. " + strings.Repeat(sentence, 12)

			persons := ofType(analyze(t, baseURL, langFr, text), entityPerson)

			requireCovered(t, persons, offset, offset+utf8.RuneCountInString(first))
			secondStart := offset + utf8.RuneCountInString(first+", ")
			requireCovered(t, persons, secondStart, secondStart+utf8.RuneCountInString(second))
			for _, person := range persons {
				require.Equal(t, personRecognizer, person.Explanation.Recognizer)
				require.GreaterOrEqual(t, person.Start, offset)
				require.LessOrEqual(t, person.End, secondStart+utf8.RuneCountInString(second))
			}
		})
	}
}

// requireCovered checks that every character from start to end is in one of the findings.
func requireCovered(t *testing.T, findings []finding, start, end int) {
	t.Helper()
	for position := start; position < end; position++ {
		covered := false
		for _, f := range findings {
			if f.Start <= position && position < f.End {
				covered = true
				break
			}
		}
		require.Truef(t, covered, "character %d is in no finding: %+v", position, findings)
	}
}

func Test_Analyzer_French_Names_InProse_AroundAChunkBoundary(t *testing.T) {
	baseURL := startAnalyzer(t)
	sentence := "Le colis a été déposé au guichet avant midi. "
	// Whole sentences of several lengths move the name across the end of the first chunk.
	shifts := []string{"", "Vu. ", "Bien reçu. ", "Dossier complet. ", "Rien à signaler ce jour. "}
	names := []string{"Corentin Delaunay", "Mathilde Rousseau", "Corentin Le Guével", "Anne Sophie Marchand"}

	for _, name := range names {
		for _, shift := range shifts {
			prefix := strings.Repeat(sentence, 8) + shift + "Le dossier a été validé par "
			offset := utf8.RuneCountInString(prefix)
			t.Run(fmt.Sprintf("%s at %d", name, offset), func(t *testing.T) {
				text := prefix + name + " hier soir. " + strings.Repeat(sentence, 12)

				findings := analyze(t, baseURL, langFr, text)

				person := requirePerson(t, text, name, findings)
				require.Equal(t, offset, person.Start)
			})
		}
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
