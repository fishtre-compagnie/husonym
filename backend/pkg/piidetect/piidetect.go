// Package piidetect fournit une détection heuristique de la nature d'une colonne
// (email, téléphone, nom, adresse, etc.) à partir de son nom et de son type SQL,
// afin de signaler les données à caractère personnel (RGPD) et de proposer un
// transformer d'anonymisation adapté.
//
// Il s'agit d'une détection SANS lecture de données : uniquement le nom de la
// colonne et son type. Elle couvre la majorité des cas réels ; un second niveau
// basé sur l'échantillonnage de contenu (Presidio) pourra la compléter plus tard.
//
// Le vocabulaire de catégories est aligné sur les domaines sémantiques du moteur
// Athanor (voir worker/pkg/athanor/runner/deterministic.go) de sorte qu'une
// suggestion hérite naturellement de la cohérence déterministe.
package piidetect

import (
	"fmt"
	"regexp"
	"strings"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
)

// Classification est le résultat de la détection pour une colonne.
type Classification struct {
	// Category est la nature sémantique détectée (ex: "email", "phone_number").
	Category string
	// Sensitive indique une donnée à caractère personnel (RGPD).
	Sensitive bool
	// Suggested est le transformer recommandé pour anonymiser la colonne.
	Suggested mgmtv1alpha1.TransformerSource
}

// rule décrit une règle de détection par mots-clés sur le nom de la colonne.
type rule struct {
	category  string
	sensitive bool
	suggested mgmtv1alpha1.TransformerSource
	// keywords matched as substrings against the normalized name.
	keywords []string
	// tokenOnly : mots-clés recherchés UNIQUEMENT comme token entier (évite les
	// faux positifs des mots courts, ex. "nom" dans "prenom", "tel" dans "hotel").
	tokenOnly []string
	// excludeTokens : si l'un de ces tokens est présent, la règle est écartée.
	// Sert à départager des règles qui se recouvrent sans casser leur ordre :
	// "nom_complet" contient le token "nom" (nom de famille) alors qu'il désigne
	// un nom complet.
	excludeTokens []string
	// suggestIfNumeric : si non nul, transformer alternatif quand le type SQL est
	// numérique (ex. téléphone stocké en entier).
	suggestIfNumeric mgmtv1alpha1.TransformerSource
	// suggestIfTemporal : transformer alternatif quand le type SQL est temporel
	// (date, timestamp...). Une date en type natif n'a pas de format d'affichage
	// à préserver : le driver écrit une vraie date. C'est ce qui permet de
	// suggérer un générateur là où une date stockée en texte l'interdit.
	suggestIfTemporal mgmtv1alpha1.TransformerSource
	// ownTokens are tokens that are part of the datum in a name this rule matches,
	// where they would otherwise say that the name is a reference or a qualifier: the
	// "code" of a postal code, the "id" of a tax id, the "key" of an API key.
	ownTokens []string
}

// objectTokens : tokens qui disent que la colonne nomme une CHOSE, pas une personne.
// "name" et "nom" ne disent pas à eux seuls ce qui est nommé, et l'immense majorité
// des colonnes qui les portent nomment un objet : file_name, product_name,
// nom_fichier. Sans ces exclusions, introspecter un schéma catalogue marque les noms
// de produits comme donnée personnelle et propose de les remplacer par des noms de
// personnes. Partagés par les règles person_last_name et person_full_name.
var objectTokens = []string{
	"file", "product", "table", "column", "field", "schema", "index",
	"host", "domain", "server", "cluster", "node", "database", "db",
	"company", "brand", "store", "shop", "site",
	"service", "app", "application", "module", "package", "class", "method",
	"project", "task", "job", "step", "rule", "policy", "role", "group",
	"type", "category", "tag", "label", "template", "theme", "style",
	"event", "queue", "topic", "bucket", "folder", "directory", "path",
	"param", "variable", "attribute", "property", "status", "state",
	"image", "icon", "color", "currency", "unit", "measure",
	// Équivalents français des plus courants.
	"fichier", "produit", "societe", "marque", "magasin",
	"projet", "tache", "regle", "groupe", "categorie", "etiquette",
	"modele", "evenement", "dossier", "chemin", "etat", "devise", "unite",
	// German, Spanish, Italian, Dutch, Polish and Portuguese words for the same things.
	"datei", "produkt", "firma", "marke", "projekt", "gruppe", "kategorie", "vorlage", "ordner", "pfad",
	"rolle", "aufgabe", "tabelle", "spalte",
	"archivo", "empresa", "marca", "tienda", "proyecto", "tarea", "regla", "grupo", "categoria",
	"producto", //nolint:misspell // a Spanish word
	"etiqueta", "plantilla", "evento", "carpeta", "ruta", "tabla", "columna", "campo",
	"prodotto", "azienda", "societa", "negozio", "progetto", "gruppo", "etichetta", "modello", "cartella",
	"percorso", "tabella", "colonna",
	"bestand", "bedrijf", "merk", "winkel", "taak", "regel", "groep", "categorie", "sjabloon", "map", "pad",
	"tabel", "kolom", "veld",
	"plik", "pliku", "produktu", "marka", "sklep", "zadanie", "regula", "grupa", "kategoria", "etykieta",
	"szablon", "sciezka", "tabela", "kolumna", "pole",
	"arquivo", "ficheiro", "produto", "loja", "projeto", "tarefa", "regra", "modelo", "pasta", "caminho",
	"coluna",
}

// The words a code is called by. In most names a code qualifies (country_code); in a
// few it is the datum (postal_code, pin_code).
var codeTokens = []string{"code", "codigo", "codice", "kod"}

// L'ordre est significatif : première règle qui matche = gagnante. Les règles les
// plus spécifiques (username, prénom) précèdent les plus génériques (nom, name).
var rules = []rule{
	{
		// What lets someone act as a person: a password, a token, a key, a secret, in
		// clear or hashed. No transformer is suggested: none keeps a hash valid.
		category:  "secret",
		sensitive: true,
		suggested: mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_UNSPECIFIED,
		keywords: []string{
			"password", "passwd", "passphrase", "motdepasse", "passwort", "kennwort", "contrasena", "claveacceso",
			"senha", "palavrapasse", "wachtwoord", "haslo", "parolachiave", "paroladordine",
			"apikey", "apitoken", "accesstoken", "refreshtoken", "authtoken", "bearertoken", "sessiontoken",
			"secretkey", "privatekey", "clientsecret", "securitycode", "credential",
		},
		tokenOnly: []string{
			"token", "secret", "pwd", "mdp", "jeton", "pin", "clave",
			"secreto", "segreto", "segredo", "geheim", "sekret",
		},
		// clave_primaria, clave_foranea: the keys of a table, not of a person.
		excludeTokens: []string{"primaria", "foranea", "externa"},
		ownTokens:     append([]string{"key"}, codeTokens...),
	},
	{
		category:  "email",
		sensitive: true,
		suggested: mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_GENERATE_EMAIL,
		keywords:  []string{"email", "mail", "courriel", "correo", "correio", "postaelettronica", "pocztaelektroniczna"},
	},
	{
		// Transform, not Generate: its default keeps the prefix, separators and length of
		// the source number (06…, +33 6…) and gives distinct numbers distinct outputs.
		category:         "phone_number",
		sensitive:        true,
		suggested:        mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_TRANSFORM_PHONE_NUMBER,
		suggestIfNumeric: mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_GENERATE_INT64_PHONE_NUMBER,
		keywords: []string{
			"phone", "telephone", "mobile", "cellphone",
			"telefon", "telefoon", "mobil", "mobiel", "movil", "celular", "cellular", "telemovel", "komork",
			"rufnummer",
		},
		tokenOnly: []string{"tel", "gsm", "fax", "handy"},
	},
	{
		category:  "username",
		sensitive: true,
		suggested: mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_GENERATE_USERNAME,
		keywords: []string{
			"username", "login",
			"benutzername", "nutzername", "nombreusuario", "nombredeusuario", "nomeutente", "gebruikersnaam",
			"nazwauzytkownika", "nomeutilizador", "nomeusuario", "nomedeusuario", "nomdutilisateur",
		},
		tokenOnly: []string{"user", "pseudo", "usuario", "utilizador", "identifiant"},
		// created_by_user, updated_by_user : colonnes d'audit qui référencent un
		// utilisateur, pas son login.
		excludeTokens: []string{"by"},
	},
	{
		// A name that says it is whole, before the rules of its parts: nombre_completo
		// holds the word of a first name, imie_i_nazwisko those of both.
		category:  "person_full_name",
		sensitive: true,
		suggested: mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_GENERATE_FULL_NAME,
		keywords: []string{
			"nombrecompleto", "nomecompleto", "imieinazwisko", "vollstaendigername", "vollername",
			"volledigenaam",
		},
	},
	{
		category:  "person_first_name",
		sensitive: true,
		suggested: mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_GENERATE_FIRST_NAME,
		keywords: []string{
			"firstname", "givenname", "forename", "prenom",
			"vorname", "rufname", "primernombre", "nombredepila", "voornaam", "primeironome",
		},
		// "nombre" is a first name in Spanish and a count in French: it is reported.
		tokenOnly:     []string{"fname", "nombre", "imie"},
		excludeTokens: objectTokens,
	},
	{
		category:  "person_last_name",
		sensitive: true,
		suggested: mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_GENERATE_LAST_NAME,
		keywords: []string{
			"lastname", "surname", "familyname", "patronyme",
			"nachname", "familienname", "zuname", "apellido", "apelido", "sobrenome", "cognome", "achternaam",
			"familienaam", "nazwisko",
		},
		tokenOnly:     []string{"lname", "nom"},
		excludeTokens: append([]string{"complet", "full", "entier"}, objectTokens...),
	},
	{
		category:      "person_full_name",
		sensitive:     true,
		suggested:     mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_GENERATE_FULL_NAME,
		keywords:      []string{"fullname", "nomcomplet"},
		tokenOnly:     []string{"name", "naam", "nome"},
		excludeTokens: objectTokens,
	},
	{
		// Avant street_address : "ip_address" contient la sous-chaîne "address".
		category:  "ip_address",
		sensitive: true,
		suggested: mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_GENERATE_IP_ADDRESS,
		keywords:  []string{"ipaddress", "ipaddr"},
		tokenOnly: []string{"ip"},
	},
	{
		category:  "street_address",
		sensitive: true,
		suggested: mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_GENERATE_FULL_ADDRESS,
		keywords: []string{
			"address", "adresse", "street",
			"anschrift", "strasse", "direccion", "domicilio", "indirizz", "straat", "ulica", "enderec",
			"adres", //nolint:misspell // a Dutch and Polish word
			"morada", "logradouro",
		},
		tokenOnly: []string{"rue", "calle", "rua"},
	},
	{
		// Champs géo rapportés à une personne = donnée personnelle (RGPD).
		category:  "city",
		sensitive: true,
		suggested: mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_GENERATE_CITY,
		keywords: []string{
			"city", "ville",
			"stadt", "wohnort", "ciudad", "localidad", "citta", "woonplaats", "miasto", "miejscowosc", "cidade",
		},
		tokenOnly: []string{"ort", "stad", "plaats"},
	},
	{
		category:  "state",
		sensitive: true,
		suggested: mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_GENERATE_STATE,
		keywords:  []string{"provinc", "bundesland", "wojewodztwo"},
		tokenOnly: []string{"state", "region", "regione", "distrito"},
	},
	{
		category:  "postal_code",
		sensitive: true,
		suggested: mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_GENERATE_ZIPCODE,
		keywords:  []string{"zipcode", "zip", "postal", "postcode", "postleitzahl", "pocztow"},
		tokenOnly: []string{"cp", "plz", "cap", "cep"},
		ownTokens: codeTokens,
	},
	{
		category:  "country",
		sensitive: true,
		suggested: mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_GENERATE_COUNTRY,
		keywords:  []string{"country", "pays"},
		tokenOnly: []string{"land", "pais", "paese", "nazione", "kraj"},
	},
	{
		category:  "ssn",
		sensitive: true,
		suggested: mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_GENERATE_SSN,
		keywords: []string{
			"socialsecurity", "securitesociale",
			"sozialversicherung", "seguridadsocial", "segurancasocial",
		},
		// "ssn" as a whole word: Reisepassnummer holds its letters.
		tokenOnly: []string{"ssn", "nir"},
		ownTokens: []string{"id"},
	},
	{
		// An identifier an authority issues to a person, other than a social security
		// number. No transformer is suggested: each has a format of its own.
		category:  "national_id",
		sensitive: true,
		suggested: mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_UNSPECIFIED,
		keywords: []string{
			"taxid", "nationalid", "passport", "passeport", "pasaporte", "passaporto", "passaporte", "paspoort",
			"paszport", "reisepass", "personalausweis", "steuerid", "steueridentifikation", "steuernummer",
			"codicefiscale", "burgerservicenummer", "contribuinte",
		},
		tokenOnly: []string{"dni", "nie", "nif", "bsn", "pesel", "cpf", "curp"},
		ownTokens: append([]string{"id"}, codeTokens...),
	},
	{
		category:  "credit_card",
		sensitive: true,
		suggested: mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_GENERATE_CARD_NUMBER,
		keywords: []string{
			"cardnumber", "creditcard", "ccnumber", "cardno",
			"kreditkarte", "numerotarjeta", "tarjetacredito", "tarjetadecredito", "cartadicredito", "numerocarta",
			"numerkarty", "kartakredytowa", "numerocartao", "cartaocredito", "cartaodecredito", "cartebancaire",
		},
	},
	{
		category:  "gender",
		sensitive: true,
		suggested: mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_GENERATE_GENDER,
		keywords: []string{
			"gender", "sexe", "genre",
			"geschlecht", "anrede", "sexo", "genero", "sesso", "geslacht", "aanhef", "civilite", "salutation",
		},
		tokenOnly: []string{"plec"},
	},
	{
		// Date de naissance : la suggestion dépend du TYPE de la colonne, cf.
		// suggestIfTemporal. Voir DetectDateFormat pour l'inférence du format des
		// dates stockées en texte.
		category:  "birth_date",
		sensitive: true,
		// Aucune suggestion par défaut : sur une colonne TEXTE, un générateur de
		// timestamp écrirait "2026-07-30T14:22:31Z" là où la source contient
		// "25/12/1980", cassant le format attendu par l'applicatif cible.
		suggested: mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_UNSPECIFIED,
		// En type temporel natif en revanche, il n'y a pas de format à préserver.
		suggestIfTemporal: mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_GENERATE_UTCTIMESTAMP,
		keywords: []string{
			"birthdate", "birthday", "dateofbirth", "datenaissance",
			"datedenaissance", "naissance",
			"geburtsdatum", "geburtstag", "nacimiento", "nascita", "geboortedatum", "urodzenia", "nascimento",
		},
		tokenOnly: []string{"dob", "ddn"},
	},
}

// referenceSuffixes : dernier token d'une colonne qui référence une autre ligne
// (user_id, email_uuid, client_fk). Le nom de l'entité référencée n'en fait pas
// une donnée personnelle, et lui suggérer un générateur casserait la clé étrangère.
var referenceSuffixes = map[string]bool{"id": true, "uuid": true, "guid": true, "fk": true, "ref": true, "key": true}

var (
	nonAlnum      = regexp.MustCompile(`[^a-z0-9]+`)
	numericTypeRe = regexp.MustCompile(`int|serial|numeric|decimal|number|float|double|real`)
	// timestamptz, datetime2, smalldatetime... sont couverts par les racines.
	temporalTypeRe = regexp.MustCompile(`date|timestamp|datetime`)
)

// normalize met le nom en minuscules et retire les séparateurs (garde a-z0-9).
func normalize(name string) string {
	return nonAlnum.ReplaceAllString(fold(name), "")
}

// tokenize découpe le nom en tokens sur les séparateurs et les frontières de casse
// (camelCase). Ex: "customerEmail_2" -> ["customer","email","2"].
func tokenize(name string) []string {
	var out []string
	var cur strings.Builder
	var prevLower bool
	for _, r := range accents.Replace(name) {
		switch {
		case r >= 'A' && r <= 'Z':
			if prevLower && cur.Len() > 0 {
				out = append(out, cur.String())
				cur.Reset()
			}
			cur.WriteRune(r - 'A' + 'a')
			prevLower = false
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			cur.WriteRune(r)
			prevLower = r >= 'a' && r <= 'z'
		default:
			if cur.Len() > 0 {
				out = append(out, cur.String())
				cur.Reset()
			}
			prevLower = false
		}
	}
	if cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out
}

func isNumericType(dataType string) bool {
	return numericTypeRe.MatchString(strings.ToLower(dataType))
}

// isTemporalType reconnaît les types date/heure natifs, par opposition à une date
// stockée dans une colonne texte.
func isTemporalType(dataType string) bool {
	return temporalTypeRe.MatchString(strings.ToLower(dataType))
}

// Classify retourne la classification d'une colonne à partir de son nom et de son
// type SQL. ok vaut false si aucune règle ne matche.
func Classify(columnName, dataType string) (Classification, bool) {
	norm := normalize(columnName)
	if norm == "" {
		return Classification{}, false
	}
	tokens := tokenize(columnName)
	tokenSet := make(map[string]struct{}, len(tokens))
	for _, t := range tokens {
		tokenSet[t] = struct{}{}
	}

	for i := range rules {
		ru := &rules[i]
		excluded := false
		for _, ex := range ru.excludeTokens {
			if _, ok := tokenSet[ex]; ok {
				excluded = true
				break
			}
		}
		if excluded {
			continue
		}

		matched := false
		for _, kw := range ru.keywords {
			if strings.Contains(norm, kw) {
				matched = true
				break
			}
		}
		if !matched {
			for _, kw := range ru.tokenOnly {
				if _, ok := tokenSet[kw]; ok {
					matched = true
					break
				}
			}
		}
		if !matched {
			continue
		}

		// The first rule that matches decides, also that the name is no finding: a
		// reference to another row (user_id, email_uuid) or a qualifier of the datum
		// (email_format). A later rule does not get a name an earlier one set aside.
		own := wordSet(ru.ownTokens...)
		last := tokens[len(tokens)-1]
		if len(tokens) > 1 && referenceSuffixes[last] && !own[last] {
			return Classification{}, false
		}
		if qualifies(tokens, own) {
			return Classification{}, false
		}

		suggested := ru.suggested
		if ru.suggestIfNumeric != mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_UNSPECIFIED &&
			isNumericType(dataType) {
			suggested = ru.suggestIfNumeric
		}
		if ru.suggestIfTemporal != mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_UNSPECIFIED &&
			isTemporalType(dataType) {
			suggested = ru.suggestIfTemporal
		}
		return Classification{
			Category:  ru.category,
			Sensitive: ru.sensitive,
			Suggested: suggested,
		}, true
	}
	return Classification{}, false
}

// SuggestionForEntity maps a Presidio entity (content analysis) to a
// Classification (category, sensitivity, suggested transformer). ok is false when
// the entity has no suitable transformer. dataType selects the numeric flavor of
// the phone transformer.
func SuggestionForEntity(entity, dataType string) (Classification, bool) {
	switch strings.ToUpper(entity) {
	case "EMAIL_ADDRESS":
		return Classification{"email", true, mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_GENERATE_EMAIL}, true
	case "PHONE_NUMBER":
		src := mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_GENERATE_STRING_PHONE_NUMBER
		if isNumericType(dataType) {
			src = mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_GENERATE_INT64_PHONE_NUMBER
		}
		return Classification{"phone_number", true, src}, true
	case "PERSON":
		return Classification{"person_full_name", true, mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_GENERATE_FULL_NAME}, true
	case "LOCATION", "LOCATION_CITY", "GPE":
		return Classification{"city", true, mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_GENERATE_CITY}, true
	case "CREDIT_CARD":
		return Classification{"credit_card", true, mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_GENERATE_CARD_NUMBER}, true
	case "IP_ADDRESS":
		return Classification{"ip_address", true, mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_GENERATE_IP_ADDRESS}, true
	case "US_SSN", "FR_NIR":
		return Classification{"nir", true, mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_GENERATE_SSN}, true
	case "FR_PHONE_NUMBER":
		src := mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_GENERATE_STRING_PHONE_NUMBER
		if isNumericType(dataType) {
			src = mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_GENERATE_INT64_PHONE_NUMBER
		}
		return Classification{"phone_number", true, src}, true
	case "FR_POSTAL_CODE":
		return Classification{"postal_code", true, mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_GENERATE_ZIPCODE}, true
	case "IBAN_CODE":
		// Pas de générateur d'IBAN : on signale la sensibilité sans suggérer de
		// transformer, plutôt que d'en imposer un qui produirait un IBAN invalide.
		return Classification{"iban", true, mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_UNSPECIFIED}, true
	case "FR_SIRET":
		return Classification{"siret", true, mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_UNSPECIFIED}, true
	// DATE_TIME est volontairement ABSENT : Presidio l'émet sur presque tout texte
	// contenant une date, y compris du texte libre truffé de PII. Comme l'entité
	// dominante est celle qui couvre le plus de valeurs, DATE_TIME supplanterait
	// PERSON sur une colonne de commentaires et la ferait passer pour non
	// sensible. Les dates sont traitées en amont, de façon déterministe, par
	// DetectDateFormat.
	default:
		return Classification{}, false
	}
}

// Enrich annote chaque colonne avec sa catégorie détectée, son flag RGPD et le
// transformer suggéré. Les colonnes non reconnues restent inchangées.
func Enrich(columns []*mgmtv1alpha1.DatabaseColumn) {
	for _, col := range columns {
		if col == nil {
			continue
		}
		c, ok := Classify(col.GetColumn(), col.GetDataType())
		if !ok {
			continue
		}
		col.DataCategory = c.Category
		col.IsSensitive = c.Sensitive
		col.SuggestedTransformerSource = c.Suggested
		// La détection par nom est déterministe : elle ne dépend d'aucun modèle
		// et donne le même résultat à chaque introspection.
		col.PiiConfidence = mgmtv1alpha1.PiiConfidence_PII_CONFIDENCE_CONFIRMED
		col.PiiDetectionMethod = mgmtv1alpha1.PiiDetectionMethod_PII_DETECTION_METHOD_COLUMN_NAME
		col.PiiEvidence = fmt.Sprintf("reconnu par le nom de colonne « %s »", col.GetColumn())
	}
}
