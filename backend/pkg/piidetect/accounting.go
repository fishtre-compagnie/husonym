package piidetect

import "slices"

// An income and a wage are what a person earns, and also lines of the accounts and
// figures of statistics: net_income, income_tax, minimum_wage. A salary is always a
// person's and is a plain keyword; the words below are guarded.
var earnings = []string{
	"income", "wage", "wages", "revenu", "revenus", "einkommen", "ingresos", "reddito", "inkomen", "dochod",
	"rendimento",
}

// accountingWords make an income or a wage a line of the accounts or a figure. The list
// is closed. A word is here when it names a line of an income statement (net, gross,
// operating, interest, deferred), a tax or its base (tax, taxable, fiscal), what
// statistics compute or set (rate, average, minimum, living, national) or a
// classification of incomes (category, source, group, bracket). The period of a pay is
// not here: an annual income and an hourly wage are a person's.
var accountingWords = []string{
	// English
	"net", "gross", "operating", "interest", "deferred", "accrued", "rental", "dividend", "investment", "other",
	"total", "statement", "account", "accounts", "ledger", "ytd", "tax", "taxes", "taxable", "fiscal",
	"rate", "average", "avg", "median", "mean", "minimum", "min", "maximum", "max", "living", "national",
	"category", "source", "group", "bracket", "range", "band", "level", "class", "threshold",
	// French
	"imposable", "impot", "taux", "brut", "moyen", "categorie", "tranche", "totaux",
	// German
	"steuer", "netto", "brutto", "durchschnitt", "gruppe", "klasse",
	// Spanish
	"totales", "neto", "netos", "bruto", "brutos", "fiscales", "impuesto", "impuestos", "tasa",
	"financieros", "medio", "medios", "cuenta", "categoria", "fuente",
	// Italian
	"imponibile", "lordo", "operativo", "totale", "fiscale", "nazionale",
	// Dutch
	"belasting", "belastbaar", "totaal", "gemiddeld", "groep",
	"nationaal", //nolint:misspell // a Dutch word
	// Polish
	"narodowy", "podatek", "sredni",
	// Portuguese
	"liquido", "taxa", "tributavel", "nacional",
}

// The words of a person in English: beside them an age, an income and a birth are that
// person's.
var englishPersons = []string{
	"user", "customer", "client", "patient", "employee", "member", "person", "student", "applicant",
}

// earners say whose income it is, whatever the words of the accounts beside it.
var earners = slices.Concat(
	englishPersons,
	[]string{"household", "spouse", "worker", "staff", "emp", "borrower", "tenant", "personal"},
	frenchPersons, []string{"foyer", "menage"},
	spanishPersons, []string{"hogar"},
	[]string{"kunde", "mitarbeiter", "haushalt", "klant", "medewerker", "dipendente", "pracownik", "funcionario"},
)

// earningGuards guards every word of earnings alike.
func earningGuards() []guarded {
	out := make([]guarded, len(earnings))
	for i, word := range earnings {
		out[i] = guarded{word: word, unless: accountingWords, despite: earners}
	}
	return out
}

// birthFigures make a birth a figure of statistics: a rate of births, a weight at birth.
var birthFigures = []string{"rate", "rates", "weight", "count", "order", "cohort", "control"}
