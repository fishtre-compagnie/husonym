package piidetect

import "testing"

// What a person is paid is personal data, whatever the word and the period.
func TestClassify_ThePayOfAPerson(t *testing.T) {
	expectCategories(t, map[string]string{
		"salary": "salary", "salaire": "salary", "gehalt": "salary", "salario": "salary", "stipendio": "salary",
		"salaris":       "salary", //nolint:misspell // a Dutch word
		"wynagrodzenie": "salary", "base_salary": "salary", "monthly_salary": "salary",
		"income": "salary", "wage": "salary", "wages": "salary", "revenu": "salary", "ingresos": "salary",
		"annual_income": "salary", "employee_income": "salary", "household_income": "salary",
		"customer_income": "salary", "user_wage": "salary", "hourly_wage": "salary",
		"revenu_mensuel": "salary", "ingresos_cliente": "salary", "annualincome": "salary",
	})
}

// An income and a wage are also lines of the accounts and figures of statistics: a word
// of the accounts or of statistics beside them makes the name something else.
func TestClassify_AnIncomeOfTheAccounts(t *testing.T) {
	expectNone(t,
		"net_income", "gross_income", "income_tax", "income_tax_rate", "income_account",
		"income_statement_line", "interest_income", "operating_income", "deferred_income",
		"income_category", "income_source", "income_ytd", "total_income", "other_income",
		"minimum_wage", "living_wage", "wage_rate", "wage_group", "average_wage",
		"revenu_fiscal", "revenu_imposable", "ingresos_totales", "ingresos_netos",
		"reddito_imponibile", "dochod_narodowy", "rendimento_liquido",
		"incometax", "incomeaccount", "NetIncome", "IncomeTaxRate",
	)
}

// The word of a person says whose income it is, whatever the other words are.
func TestClassify_AnIncomeOfTheAccountsOfAPerson(t *testing.T) {
	expectCategories(t, map[string]string{
		"employee_gross_income": "salary", "customer_net_income": "salary", "household_total_income": "salary",
		"user_wage_rate": "salary", "revenu_fiscal_client": "salary", "ingresos_netos_cliente": "salary",
	})
}

// A salary keeps its finding beside the words of the accounts: it is always a person's.
func TestClassify_ASalaryBesideTheWordsOfTheAccounts(t *testing.T) {
	expectCategories(t, map[string]string{
		"net_salary": "salary", "gross_salary": "salary", "salaire_brut": "salary", "salaire_net": "salary",
	})
}

// A birth is a person's; a rate of births and a weight at birth are figures, and what is
// born digital is a document.
func TestClassify_ABirthOfStatistics(t *testing.T) {
	expectCategories(t, map[string]string{
		"date_of_birth": "birth_date", "birth_date": "birth_date", "birth_place": "birth_date",
		"birth_year": "birth_date", "birth": "birth_date", "born": "birth_date",
		"born_on": "birth_date", "patient_birth_weight": "birth_date",
		// The name someone was born with is a name.
		"birth_name": "person_full_name",
	})
	expectNone(t, "birth_rate", "birth_weight", "born_digital", "birthrate", "BirthWeight")
}
