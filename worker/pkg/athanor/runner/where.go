package runner

import mgmtv1alpha1 "github.com/Groupe-Hevea/neosync/backend/gen/go/protos/mgmt/v1alpha1"

// WhereForTable renvoie la clause de subsetting (sans le mot-clé WHERE) configurée
// pour une table donnée dans les options de source du job, ou "" si aucune.
//
// La structure est identique pour PostgreSQL, MySQL et MSSQL : options de source
// -> schemas[] -> tables[] -> where_clause. On la parcourt selon le SGBD de la source.
func WhereForTable(source *mgmtv1alpha1.JobSource, schema, table string) string {
	if source == nil {
		return ""
	}
	opts := source.GetOptions()
	switch {
	case opts.GetMysql() != nil:
		for _, s := range opts.GetMysql().GetSchemas() {
			if s.GetSchema() != schema {
				continue
			}
			for _, t := range s.GetTables() {
				if t.GetTable() == table {
					return t.GetWhereClause()
				}
			}
		}
	case opts.GetPostgres() != nil:
		for _, s := range opts.GetPostgres().GetSchemas() {
			if s.GetSchema() != schema {
				continue
			}
			for _, t := range s.GetTables() {
				if t.GetTable() == table {
					return t.GetWhereClause()
				}
			}
		}
	case opts.GetMssql() != nil:
		for _, s := range opts.GetMssql().GetSchemas() {
			if s.GetSchema() != schema {
				continue
			}
			for _, t := range s.GetTables() {
				if t.GetTable() == table {
					return t.GetWhereClause()
				}
			}
		}
	}
	return ""
}
