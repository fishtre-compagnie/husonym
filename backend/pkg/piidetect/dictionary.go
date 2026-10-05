package piidetect

import (
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
)

// rule finds one kind of datum from the words of a column name.
type rule struct {
	category  string
	sensitive bool
	// suggested is the transformer suggested for a column that holds text, or whose
	// type is not given. Every rule has one. See suggestionFor for the other types.
	suggested mgmtv1alpha1.TransformerSource
	// keywords are words, or several words that follow each other, written with spaces.
	// See pattern for "word*" and "*word".
	keywords []string
	// guarded are keywords that are ordinary words in other names.
	guarded []guarded
	// excludeTokens set the rule aside when the name holds one of them: "nom_complet"
	// holds the word of a last name and is a full name.
	excludeTokens []string
	// suggestIfInteger is the transformer of the datum for an integer column, when one
	// exists (a phone number stored as an integer).
	suggestIfInteger mgmtv1alpha1.TransformerSource
	// suggestIfTemporal is the transformer of the datum for a column that holds dates or
	// times: a native date has no display format to keep.
	suggestIfTemporal mgmtv1alpha1.TransformerSource
	// ownTokens are words that are part of the datum in a name this rule matches, where
	// they would otherwise say that the name is a reference or a qualifier: the "code"
	// of a postal code, the "id" of a tax id, the "key" of an API key.
	ownTokens []string
	// alsoHolds are the kinds of column, other than text, that can hold the datum. A
	// rule is not applied to a column of another kind: no first name is an integer.
	alsoHolds []columnKind
}

// guarded is a keyword of one word that names the datum in some names and something
// ordinary in others. As the whole name it matches, unless beside is set. Beside other
// words it matches when one of among is there, if among is given, and none of unless;
// never when alone is set. One of despite beside it sets unless aside: it says whose
// datum it is. With apart, it is not a word a glued token is cut into.
type guarded struct {
	word    string
	among   []string
	unless  []string
	despite []string
	alone   bool
	beside  bool
	apart   bool
}

// objectTokens say that the column names a thing, not a person. "name" and "nom" do not
// say what is named, and most columns that hold them name an object: file_name,
// product_name, nom_fichier.
var objectTokens = []string{
	"file", "product", "table", "column", "field", "schema", "index",
	"host", "domain", "server", "cluster", "node", "database", "db",
	"company", "brand", "store", "shop", "site", "business", "bank", "hotel",
	"service", "app", "application", "module", "package", "class", "method", "process",
	"project", "task", "job", "step", "rule", "policy", "role", "group",
	"type", "category", "tag", "label", "template", "theme", "style",
	"event", "queue", "topic", "bucket", "folder", "directory", "path",
	"param", "variable", "attribute", "property", "status", "state",
	"image", "icon", "color", "currency", "unit", "measure",
	"plan", "item", "sku", "menu", "article", "course", "campaign", "feature", "warehouse",
	// French
	"fichier", "produit", "societe", "marque", "magasin",
	"projet", "tache", "regle", "groupe", "categorie", "etiquette",
	"modele", "evenement", "dossier", "chemin", "etat", "devise", "unite",
	"banque", "cours", "entreprise", "commercial", "domaine",
	// German
	"datei", "produkt", "firma", "marke", "projekt", "gruppe", "kategorie", "vorlage", "ordner", "pfad",
	"rolle", "aufgabe", "tabelle", "spalte", "artikel", "kurs", "unternehmen",
	// Spanish
	"archivo", "empresa", "marca", "tienda", "proyecto", "tarea", "regla", "grupo", "categoria",
	"producto", //nolint:misspell // a Spanish word
	"etiqueta", "plantilla", "evento", "carpeta", "ruta", "tabla", "columna", "campo",
	"banco", "articulo", "curso",
	// Italian
	"prodotto", "azienda", "societa", "negozio", "progetto", "gruppo", "etichetta", "modello", "cartella",
	"percorso", "tabella", "colonna", "banca", "corso", "piano", "articolo",
	// Dutch
	"bestand", "bedrijf", "merk", "winkel", "taak", "regel", "groep", "sjabloon", "map", "pad",
	"tabel", "kolom", "veld", "cursus",
	// Polish
	"plik", "pliku", "produktu", "marka", "sklep", "zadanie", "regula", "grupa", "kategoria", "etykieta",
	"szablon", "sciezka", "tabela", "kolumna", "pole",
	// Portuguese
	"arquivo", "ficheiro", "produto", "loja", "projeto", "tarefa", "regra", "modelo", "pasta", "caminho",
	"coluna", "artigo",
}

// The words a code is called by. In most names a code qualifies (country_code); in a
// few it is the datum (postal_code, pin_code).
var codeTokens = []string{"code", "codes", "codigo", "codice", "kod"}

// The words of a person in Spanish and in French, beside which "nombre", "genero" and
// "genre" are about that person.
var (
	spanishPersons = []string{
		"apellido", "apellidos", "cliente", "usuario", "persona", "empleado", "contacto", "titular", "paciente",
		"alumno", "pila", "primer", "segundo", "propio", "padre", "madre", "padres",
	}
	frenchPersons = []string{"client", "utilisateur", "personne", "patient", "salarie", "employe", "contact"}
)

const unspecified = mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_UNSPECIFIED

// The order counts: the first rule that matches a name and does not set it aside
// decides. The more specific rules (username, first name) come before the more general
// ones (last name, name).
//
// A word is written here as its language writes it, marks included; the rules read it in
// every spelling a name gives it (see spellings).
var dictionary = []rule{
	{
		// What lets someone act as a person: a password, a token, a key, a secret, a code
		// sent to prove who they are, in clear or hashed. No transformer keeps a hash
		// valid: the scramble keeps its length, and what it writes opens nothing.
		category:  "secret",
		sensitive: true,
		suggested: scrambleText,
		keywords: []string{
			"password", "passwd", "passphrase", "mot de passe", "mot passe", "motdepasse", "passwort", "kennwort",
			"contrasena", "senha", "palavra passe", "wachtwoord", "haslo", "hasla", "parola chiave",
			"parola dordine", "parola d ordine",
			"api key", "api token", "access token", "refresh token", "auth token", "bearer token", "session token",
			"secret key", "private key", "client secret", "access key", "encryption key", "ssh key",
			"security code", "verification code", "auth code", "access code", "code acces", "recovery code",
			"recovery codes", "backup code", "backup codes", "security answer", "security question",
			"credential", "secret", "pwd", "mdp", "jeton", "salt", "passcode", "otp", "totp",
			// The code printed on a payment card.
			"cvv", "cvc",
			"secreto", "segreto", "segredo", "geheim", "sekret",
		},
		guarded: []guarded{
			// A token that turns a page or resumes a sync opens nothing.
			{word: "token", unless: []string{
				"page", "next", "continuation", "sync", "cursor", "usage", "previous", "prev",
			}},
			// A pin on a map.
			{word: "pin", unless: []string{"map", "lat", "lng", "lon", "color", "icon", "x", "y", "location"}},
			// Spanish "clave" is also the key of a thing: clave_unidad.
			{word: "clave", among: []string{"acceso", "usuario", "secreta", "privada", "cifrada", "hash", "api"}},
			// "pass" is also a boarding pass and a pass rate.
			{word: "pass", among: []string{"hash", "hashed", "user", "login", "salt", "word", "admin", "crypt"}},
		},
		ownTokens: append([]string{"key"}, codeTokens...),
		// A token is often drawn as a UUID, and stored in a column of that type.
		alsoHolds: []columnKind{kindNumber, kindReference},
	},
	{
		category:  "email",
		sensitive: true,
		suggested: mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_GENERATE_EMAIL,
		keywords: []string{
			"email*", "mail", "e mail", "gmail", "mailbox", "courriel", "correo", "correio",
			"posta elettronica", "poczta elektroniczna",
		},
	},
	{
		// Transform, not Generate: its default keeps the prefix, separators and length of
		// the source number (06…, +33 6…) and gives distinct numbers distinct outputs.
		category:         "phone_number",
		sensitive:        true,
		suggested:        mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_TRANSFORM_PHONE_NUMBER,
		suggestIfInteger: mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_GENERATE_INT64_PHONE_NUMBER,
		keywords: []string{
			"phone", "telephone", "cellphone",
			"telefon*", "telefoon*", "mobil", "mobiel*", "movil", "celular", "cellulare",
			"telemovel", "komork*", "rufnummer", "gsm", "fax", "handy", "tlf",
		},
		guarded: []guarded{
			// A mobile is a phone; what is mobile is not one.
			{word: "mobile", unless: []string{
				"app", "apps", "device", "devices", "os", "web", "browser", "platform", "sdk", "money", "banking",
				"first", "friendly",
			}},
			{word: "cellular", unless: []string{"network", "data", "plan", "carrier", "provider"}},
			// French "tel quel": as it is.
			{word: "tel", unless: []string{"quel", "aviv"}},
			// French "portable" is a mobile phone, English "portable" an adjective.
			{word: "portable", among: []string{"numero", "num", "telephone", "client", "contact"}},
		},
		alsoHolds: []columnKind{kindNumber},
	},
	{
		category:  "username",
		sensitive: true,
		suggested: mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_GENERATE_USERNAME,
		keywords: []string{
			"username", "user name", "user names", "login", "nickname",
			"benutzername", "nutzername", "nombre usuario", "nombre de usuario", "nome utente",
			"gebruikersnaam", "nazwa uzytkownika", "nome utilizador", "nome usuario", "nome de usuario",
			"nom utilisateur", "nom d utilisateur", "nom dutilisateur",
			"pseudo", "identifiant",
		},
		guarded: []guarded{
			// Alone, the user is its login; beside other words it says whose datum it is.
			{word: "user", alone: true},
			{word: "usuario", alone: true},
			{word: "utilizador", alone: true},
		},
		// created_by_user, updated_by: audit columns that refer to a user. last_login:
		// when, not who.
		excludeTokens: []string{"by", "last"},
	},
	{
		// A name that says it is whole, before the rules of its parts: nombre_completo
		// holds the word of a first name, imie_i_nazwisko those of both.
		category:  "person_full_name",
		sensitive: true,
		suggested: mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_GENERATE_FULL_NAME,
		keywords: []string{
			"nombre completo", "nome completo", "imie i nazwisko", "imieinazwisko", "vollständiger name",
			"voller name", "volledige naam",
		},
	},
	{
		category:  "person_first_name",
		sensitive: true,
		suggested: mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_GENERATE_FIRST_NAME,
		keywords: []string{
			"first name", "given name", "forename", "prenom", "fname",
			"vorname", "rufname", "primer nombre", "nombre de pila", "nombre pila", "voornaam", "primeiro nome",
			"imie",
		},
		guarded: []guarded{
			// Spanish "nombre" is a first name, French "nombre" a count.
			{word: "nombre", among: append([]string{"y"}, spanishPersons...)},
			{word: "nombres", among: append([]string{"y"}, spanishPersons...)},
		},
		excludeTokens: objectTokens,
	},
	{
		category:  "person_last_name",
		sensitive: true,
		suggested: mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_GENERATE_LAST_NAME,
		keywords: []string{
			"last name", "surname", "family name", "patronyme", "lname",
			"nachname", "familienname", "zuname", "apellido", "apelido", "sobrenome", "cognome", "achternaam",
			"familienaam", "nazwisko", "nom", "nom de famille", "nom famille",
			// The name someone was born with, and the Dutch particle of a last name.
			"maiden name", "nom de jeune fille", "nom jeune fille", "geburtsname", "tussenvoegsel", "tussenvoegsels",
		},
		excludeTokens: append([]string{"complet", "full", "entier"}, objectTokens...),
	},
	{
		category:  "person_full_name",
		sensitive: true,
		suggested: mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_GENERATE_FULL_NAME,
		keywords: []string{
			"full name", "nom complet", "name", "naam", "nome",
			// Who holds a card or an account, and who to call for someone: a person.
			"cardholder", "card holder", "account holder", "kontoinhaber", "karteninhaber", "titulaire",
			"intestatario", "emergency contact", "contact urgence", "notfallkontakt", "contacto emergencia",
		},
		excludeTokens: objectTokens,
	},
	{
		// Before street_address: "ip_address" holds the word "address".
		category:  "ip_address",
		sensitive: true,
		suggested: mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_GENERATE_IP_ADDRESS,
		keywords:  []string{"ipaddress", "ipaddr", "ip", "ipv"},
		alsoHolds: []columnKind{kindNumber},
	},
	{
		// The address of a network card identifies a device, and through it its owner. No
		// generator writes one: the scramble keeps its length and its separators.
		category:  "mac_address",
		sensitive: true,
		suggested: scrambleText,
		keywords:  []string{"mac address", "mac addr", "mac adresse", "adresse mac", "direccion mac", "indirizzo mac"},
	},
	{
		category:  "street_address",
		sensitive: true,
		suggested: mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_GENERATE_FULL_ADDRESS,
		keywords: []string{
			"address", "addresses", "addr", "street", "house number", "house no",
			"adres", //nolint:misspell // a Dutch and Polish word
			"adresse", "adresses", "adressen", "adresu", "adresy", "adresow",
			"adress", //nolint:misspell // a German and Swedish spelling
			"anschrift", "strasse", "hausnummer", "direccion", "direcciones", "domicilio", "indirizz*", "straat",
			"huisnummer", "ulica", "enderec*", "morada", "logradouro", "rue", "calle", "rua",
		},
		guarded: []guarded{
			// Italian "via" is a street, English "via" a way through.
			{word: "via", among: []string{"indirizzo", "civico", "cap", "comune", "citta", "residenza", "numero"}},
		},
	},
	{
		// A place, when it is where a person lives, is personal data.
		category:  "city",
		sensitive: true,
		suggested: mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_GENERATE_CITY,
		keywords: []string{
			"city", "cities", "town", "ville", "commune",
			"stadt", "wohnort", "ort", "ciudad", "localidad", "municipio", "citta", "comune", "localita",
			"woonplaats", "stad", "plaats", "gemeente", "miasto", "miejscowosc", "cidade", "localidade",
		},
	},
	{
		// Before state: Spanish "estado civil" holds the word of a state. The status is
		// the datum here, not a qualifier of it.
		category:  "marital_status",
		sensitive: true,
		suggested: scrambleText,
		keywords: []string{
			"marital", "marital status", "civil status", "estado civil", "etat civil", "situation familiale",
			"situation matrimoniale", "familienstand", "stato civile", "burgerlijke staat", "stan cywilny",
		},
		ownTokens: []string{"status", "estado", "etat", "stato", "stan"},
	},
	{
		category:  "state",
		sensitive: true,
		suggested: mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_GENERATE_STATE,
		keywords:  []string{"provinc*", "bundesland", "wojewodztwo", "regione", "distrito"},
		guarded: []guarded{
			{word: "state", unless: []string{"order", "workflow", "machine", "task", "job", "sync", "run"}},
			{word: "region", unless: []string{"aws", "cloud", "gcp", "azure"}},
			// Spanish and Portuguese "estado" is a state beside the words of an address,
			// a status elsewhere.
			{word: "estado", beside: true, among: []string{
				"direccion", "endereco", "domicilio", "ciudad", "cidade", "provincia", "pais", "residencia",
				"morada", "municipio", "cep", "nacimiento", "envio", "entrega", "facturacion",
			}},
		},
		ownTokens: []string{"estado"},
	},
	{
		// The code of the state of an address is how that state is written. The code of
		// a state on its own (state_code) is the key of a list of states.
		category:  "state",
		sensitive: true,
		suggested: mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_GENERATE_STATE,
		keywords:  []string{"address state", "address province", "address region"},
		ownTokens: codeTokens,
	},
	{
		category:  "postal_code",
		sensitive: true,
		suggested: mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_GENERATE_ZIPCODE,
		keywords:  []string{"zipcode", "postal", "postale", "postcode", "post code", "postleitzahl", "pocztow*", "plz", "cep"},
		guarded: []guarded{
			{word: "zip", unless: []string{"file", "archive", "size", "path"}},
			// French "cp" is also paid leave.
			{word: "cp", unless: []string{"solde", "acquis", "pris", "restant"}},
			// Italian "cap" is a postal code, English "cap" a limit.
			{word: "cap", among: []string{
				"comune", "citta", "indirizzo", "provincia", "residenza", "via", "localita", "spedizione",
				"fatturazione", "domicilio",
			}},
		},
		ownTokens: codeTokens,
		alsoHolds: []columnKind{kindNumber},
	},
	{
		category:  "country",
		sensitive: true,
		suggested: mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_GENERATE_COUNTRY,
		keywords: []string{
			"country", "countries", "pays", "pais", "paese", "nazione", "kraj",
			"geburtsland", "herkunftsland", "heimatland", "wohnland",
			// The country someone is a citizen of.
			"nationalite", //nolint:misspell // a French word
			"nationality", "citizenship", "citoyennete", "nationalität", "staatsangehörigkeit",
			"nacionalidad", "ciudadania", "nazionalita", "cittadinanza", "nationaliteit", "narodowosc",
			"obywatelstwo", "nacionalidade", "cidadania",
		},
		guarded: []guarded{
			// German and Dutch "land" is a country, English "land" is ground.
			{word: "land", among: []string{
				"ort", "stadt", "plz", "strasse", "wohnort", "postleitzahl", "anschrift", "kunde", "kunden",
				"straat", "plaats", "postcode", "woonplaats", "stad", "klant",
			}},
		},
	},
	{
		category:  "ssn",
		sensitive: true,
		suggested: mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_GENERATE_SSN,
		keywords: []string{
			// "ssn" opens or closes a word (ssnum, empssn); inside one it is an accident
			// of spelling (classname, Reisepassnummer).
			"ssn*", "*ssn",
			"social security", "securite sociale", "num secu", "numero secu", "numero ss", "nuss",
			"sozialversicherung*", "svnr", "seguridad social", "seguranca social", "nir",
		},
		guarded: []guarded{
			// The French number of a person; "code insee" is the code of a town.
			{word: "insee", unless: []string{"code", "commune", "communes"}},
		},
		ownTokens: []string{"id"},
		alsoHolds: []columnKind{kindNumber},
	},
	{
		// An identifier an authority issues to a person, other than a social security
		// number. Each has a format of its own: the scramble keeps its length and where
		// its letters and its digits are.
		category:  "national_id",
		sensitive: true,
		suggested: scrambleText,
		keywords: []string{
			"tax id", "tax number", "tax code", "national id", "national insurance number", "nino",
			"identity card", "id card", "passport",
			"drivers license", "driver license", "driving license",
			"drivers licence", "driver licence", "driving licence", //nolint:misspell // the British spelling
			"carte identite", //nolint:misspell // French words
			"passeport", "permis de conduire", "permis conduire",
			"reisepass*", "pass nummer", "pass nr", "ausweis", "personalausweis*", "steuer id",
			"steueridentifikation*", "steuernummer", "führerschein",
			"pasaporte", "nif", "curp", "cedula", "numero fiscal",
			"passaporto", "codice fiscale", "carta identita",
			"paspoort", "bsn", "burgerservicenummer", "rijksregisternummer", "sofi nummer", "rijbewijs",
			"paszport*", "pesel", "nip", "nr dowodu", "numer dowodu", "dowod osobisty", "prawo jazdy",
			"passaporte", "cpf", "contribuinte",
			// The identifier written first, and the number of an identity document.
			"id fiscal", "id national", "id nacional", "id tax", "id number",
			"numero documento", "numero de documento", "nro documento", "num documento",
			"documento identidad", "documento de identidad", "documento identita",
		},
		guarded: []guarded{
			// Spanish identity numbers; Polish "dni" are days and "nie" is no.
			{word: "dni", among: []string{
				"numero", "num", "nro", "cliente", "usuario", "titular", "persona", "documento", "letra", "id",
			}},
			{word: "nie", among: []string{
				"numero", "num", "nro", "cliente", "usuario", "titular", "persona", "documento", "id",
			}},
			// The Chilean tax number; English "rut" is a groove.
			{word: "rut", among: []string{
				"numero", "num", "nro", "cliente", "usuario", "titular", "persona", "empresa", "dv", "id",
			}},
			// Alone, the identity document of a person; beside other words, any document.
			{word: "documento", alone: true},
			// The Brazilian identity card.
			{word: "rg", alone: true},
		},
		ownTokens: append([]string{"id"}, codeTokens...),
		alsoHolds: []columnKind{kindNumber},
	},
	{
		// The generator of card numbers writes integers: it is suggested for an integer
		// column, and a card number stored as text is scrambled.
		category:         "credit_card",
		sensitive:        true,
		suggested:        scrambleText,
		suggestIfInteger: mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_GENERATE_CARD_NUMBER,
		keywords: []string{
			"card number", "card num", "card no", "credit card", "cc number", "cc num", "ccnumber", "ccnum",
			"kreditkarte*", "numero tarjeta", "tarjeta credito", "tarjeta de credito", "carta di credito",
			"carta credito", "numero carta", "numer karty", "karta kredytowa", "numero cartao", "cartao credito",
			"cartao de credito", "carte bancaire", "numero carte",
			// When a card expires and its last digits.
			"card expiry", "card expiration", "card last",
		},
		guarded:   []guarded{{word: "cc", alone: true}},
		alsoHolds: []columnKind{kindNumber},
	},
	{
		// No generator writes bank account numbers: they are scrambled.
		category:  "iban",
		sensitive: true,
		suggested: scrambleText,
		keywords:  []string{"iban"},
		ownTokens: codeTokens,
	},
	{
		category:  "bank_account",
		sensitive: true,
		suggested: scrambleText,
		keywords: []string{
			"bank account", "account number", "account num", "compte bancaire", "numero de compte",
			"numero compte", "bankkonto", "kontonummer", "cuenta bancaria", "numero cuenta", "numero de cuenta",
			"conto bancario", "numero conto", "bankrekening", "rekeningnummer", "konto bankowe", "numer konta",
			"conta bancaria", "numero conta", "numero da conta",
		},
		alsoHolds: []columnKind{kindNumber},
	},
	{
		category:  "salary",
		sensitive: true,
		suggested: scrambleText,
		keywords: []string{
			"salary", "salaire", "gehalt", "salario", "stipendio", "wynagrodzenie", "pensja",
			"salaris", //nolint:misspell // a Dutch word
			"lohn", "sueldo",
		},
		// What someone earns, whatever it is paid as, when the name is not a line of the
		// accounts.
		guarded: earningGuards(),
		// A limit on salaries is not a salary.
		excludeTokens: []string{"cap"},
		alsoHolds:     []columnKind{kindNumber},
	},
	{
		category:  "ethnicity",
		sensitive: true,
		suggested: scrambleText,
		keywords:  []string{"ethnicity", "ethnic", "ethnie", "etnia", "ethnizität", "etniciteit"},
	},
	{
		category:  "gender",
		sensitive: true,
		suggested: mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_GENERATE_GENDER,
		keywords: []string{
			"gender", "sex", "sexe", "civilite", "salutation",
			"geschlecht", "anrede", "sexo", "sesso", "geslacht", "aanhef", "plec",
		},
		guarded: []guarded{
			// Of a person, or of a film.
			{word: "genre", among: append([]string{"code", "customer", "user", "person"}, frenchPersons...)},
			{word: "genero", among: append([]string{"codigo"}, spanishPersons...)},
		},
		// A gender is often stored as its code or its type.
		ownTokens: append([]string{"type", "typ", "tipo"}, codeTokens...),
		alsoHolds: []columnKind{kindNumber, kindBoolean},
	},
	{
		// A date stored as text has a format no generator writes back (see
		// DetectDateFormat): it is scrambled, which keeps its length and gives no date.
		// A native date gets a generated one.
		category:          "birth_date",
		sensitive:         true,
		suggested:         scrambleText,
		suggestIfTemporal: generateMoment,
		keywords: []string{
			"birth date", "birthday", "date of birth", "date naissance", "date de naissance", "date naiss",
			"naissance", "dob", "ddn", "place of birth", "year of birth",
			"geburtsdatum", "geburtstag", "nacimiento", "nascita", "geboortedatum", "urodzenia", "nascimento",
		},
		guarded: []guarded{
			{word: "birth", unless: birthFigures, despite: englishPersons},
			// What is born digital is a document.
			{word: "born", unless: []string{"digital"}},
		},
		alsoHolds: []columnKind{kindMoment, kindNumber},
	},
	{
		category:  "age",
		sensitive: true,
		suggested: scrambleText,
		guarded: []guarded{
			// The age of a person, not a class of ages nor a bound on them. Many ordinary
			// words end in "age" (paysage, postage): it is read as a word of its own only.
			{
				word:   "age",
				apart:  true,
				among:  append([]string{"years"}, englishPersons...),
				unless: []string{"group", "range", "bracket", "limit", "min", "max", "minimum", "maximum"},
			},
			{word: "edad", among: append([]string{"anos"}, spanishPersons...)},
			{word: "eta", alone: true},
			{word: "leeftijd", alone: true}, {word: "wiek", alone: true},
			{word: "idade", alone: true},
		},
		alsoHolds: []columnKind{kindNumber},
	},
}

// Words a glued token may hold beside a keyword. The list is closed: a token that holds
// a word that is neither here nor elsewhere in the rules is not cut (see lexicon.split).
var gluedWords = []string{
	// whose datum it is
	"customer", "cust", "client", "user", "emp", "employee", "patient", "member", "contact", "owner",
	"father", "mother", "spouse",
	"kunde", "kunden", "klant",
	// which one
	"home", "work", "office", "billing", "shipping", "mailing", "delivery", "current", "primary", "secondary",
	"alt", "personal", "private", "privat", "old", "reset", "residence",
	"livraison", "facturation", "electronico", "electronica", "electronique",
	// the part of it, and the number or the hash it is stored as
	"ext", "prefix", "plus", "masked", "line", "haus",
	"number", "num", "nummer", "numero", "hash", "hashed", "encrypted", "display", "digest", "hint",
	// when and where: the date and the place of a birth, the issue of a document
	"fecha", "data", "datum", "lieu", "luogo", "lugar", "miejsce", "issue",
	// what a secret opens
	"webhook", "mfa", "twofactor",
	// how a pay is counted
	"annual", "yearly", "monthly", "weekly", "hourly", "annuel", "mensuel", "jahres", "monats",
	"anual", //nolint:misspell // a Spanish and Portuguese word
	"mensual", "annuo", "mensile", "jaar", "maand", "mensal", "brut", "brutto", "bruto",
}
