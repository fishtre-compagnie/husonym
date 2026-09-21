package job

import (
	"fmt"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
)

type SqlJobSourceOpts struct {
	// Determines if the job should halt if a new column is detected that is not present in the job mappings
	HaltOnNewColumnAddition bool
	// Determines if the job should halt if a column is removed from the source database
	HaltOnColumnRemoval bool
	// Newly detected columns are set to passthrough to the destination
	PassthroughOnNewColumnAddition bool
	// AutoMap & Review: newly detected columns are mapped as the PII detection suggests, or passed
	// through when it suggests nothing; the run records each change to the job's mappings for review.
	AutoMapNewColumns             bool
	SubsetByForeignKeyConstraints bool
	SchemaOpt                     []*SchemaOptions
}

type SchemaOptions struct {
	Schema string
	Tables []*TableOptions
}
type TableOptions struct {
	Table       string
	WhereClause *string
}

func GetSqlJobSourceOpts(
	source *mgmtv1alpha1.JobSource,
) (*SqlJobSourceOpts, error) {
	switch jobSourceConfig := source.GetOptions().GetConfig().(type) {
	case *mgmtv1alpha1.JobSourceOptions_Postgres:
		if jobSourceConfig.Postgres == nil {
			return nil, nil
		}
		schemaOpt := []*SchemaOptions{}
		for _, opt := range jobSourceConfig.Postgres.Schemas {
			tableOpts := []*TableOptions{}
			for _, t := range opt.GetTables() {
				tableOpts = append(tableOpts, &TableOptions{
					Table:       t.Table,
					WhereClause: t.WhereClause,
				})
			}
			schemaOpt = append(schemaOpt, &SchemaOptions{
				Schema: opt.GetSchema(),
				Tables: tableOpts,
			})
		}
		shouldHalt := false
		shouldPassthrough := false
		autoMapNewColumns := false
		switch jobSourceConfig.Postgres.GetNewColumnAdditionStrategy().GetStrategy().(type) {
		case *mgmtv1alpha1.PostgresSourceConnectionOptions_NewColumnAdditionStrategy_HaltJob_:
			shouldHalt = true
		case *mgmtv1alpha1.PostgresSourceConnectionOptions_NewColumnAdditionStrategy_AutoMap_:
			autoMapNewColumns = true
		case *mgmtv1alpha1.PostgresSourceConnectionOptions_NewColumnAdditionStrategy_Passthrough_:
			shouldPassthrough = true
		}

		shouldHaltOnColumnRemoval := false
		if jobSourceConfig.Postgres.GetColumnRemovalStrategy().GetHaltJob() != nil {
			shouldHaltOnColumnRemoval = true
		}

		return &SqlJobSourceOpts{
			HaltOnNewColumnAddition:        shouldHalt,
			PassthroughOnNewColumnAddition: shouldPassthrough,
			AutoMapNewColumns:              autoMapNewColumns,
			HaltOnColumnRemoval:            shouldHaltOnColumnRemoval,
			SubsetByForeignKeyConstraints:  jobSourceConfig.Postgres.SubsetByForeignKeyConstraints,
			SchemaOpt:                      schemaOpt,
		}, nil
	case *mgmtv1alpha1.JobSourceOptions_Mysql:
		if jobSourceConfig.Mysql == nil {
			return nil, nil
		}
		schemaOpt := []*SchemaOptions{}
		for _, opt := range jobSourceConfig.Mysql.Schemas {
			tableOpts := []*TableOptions{}
			for _, t := range opt.GetTables() {
				tableOpts = append(tableOpts, &TableOptions{
					Table:       t.Table,
					WhereClause: t.WhereClause,
				})
			}
			schemaOpt = append(schemaOpt, &SchemaOptions{
				Schema: opt.GetSchema(),
				Tables: tableOpts,
			})
		}
		shouldHalt := false
		shouldPassthrough := false
		autoMapNewColumns := false
		switch jobSourceConfig.Mysql.GetNewColumnAdditionStrategy().GetStrategy().(type) {
		case *mgmtv1alpha1.MysqlSourceConnectionOptions_NewColumnAdditionStrategy_HaltJob_:
			shouldHalt = true
		case *mgmtv1alpha1.MysqlSourceConnectionOptions_NewColumnAdditionStrategy_AutoMap_:
			autoMapNewColumns = true
		case *mgmtv1alpha1.MysqlSourceConnectionOptions_NewColumnAdditionStrategy_Passthrough_:
			shouldPassthrough = true
		}
		shouldHaltOnColumnRemoval := false
		if jobSourceConfig.Mysql.GetColumnRemovalStrategy().GetHaltJob() != nil {
			shouldHaltOnColumnRemoval = true
		}
		return &SqlJobSourceOpts{
			HaltOnNewColumnAddition:        shouldHalt,
			PassthroughOnNewColumnAddition: shouldPassthrough,
			AutoMapNewColumns:              autoMapNewColumns,
			HaltOnColumnRemoval:            shouldHaltOnColumnRemoval,
			SubsetByForeignKeyConstraints:  jobSourceConfig.Mysql.SubsetByForeignKeyConstraints,
			SchemaOpt:                      schemaOpt,
		}, nil
	case *mgmtv1alpha1.JobSourceOptions_Mssql:
		if jobSourceConfig.Mssql == nil {
			return nil, nil
		}
		schemaOpt := []*SchemaOptions{}
		for _, opt := range jobSourceConfig.Mssql.Schemas {
			tableOpts := []*TableOptions{}
			for _, t := range opt.GetTables() {
				tableOpts = append(tableOpts, &TableOptions{
					Table:       t.Table,
					WhereClause: t.WhereClause,
				})
			}
			schemaOpt = append(schemaOpt, &SchemaOptions{
				Schema: opt.GetSchema(),
				Tables: tableOpts,
			})
		}
		shouldHaltOnColumnRemoval := false
		if jobSourceConfig.Mssql.GetColumnRemovalStrategy().GetHaltJob() != nil {
			shouldHaltOnColumnRemoval = true
		}

		shouldHaltNewColumnAddition := false
		shouldPassthrough := false
		autoMapNewColumns := false
		switch jobSourceConfig.Mssql.GetNewColumnAdditionStrategy().GetStrategy().(type) {
		case *mgmtv1alpha1.MssqlSourceConnectionOptions_NewColumnAdditionStrategy_HaltJob_:
			shouldHaltNewColumnAddition = true
		case *mgmtv1alpha1.MssqlSourceConnectionOptions_NewColumnAdditionStrategy_Passthrough_:
			shouldPassthrough = true
		case *mgmtv1alpha1.MssqlSourceConnectionOptions_NewColumnAdditionStrategy_AutoMap_:
			autoMapNewColumns = true
		}
		return &SqlJobSourceOpts{
			HaltOnNewColumnAddition:        shouldHaltNewColumnAddition,
			PassthroughOnNewColumnAddition: shouldPassthrough,
			AutoMapNewColumns:              autoMapNewColumns,
			HaltOnColumnRemoval:            shouldHaltOnColumnRemoval,
			SubsetByForeignKeyConstraints:  jobSourceConfig.Mssql.SubsetByForeignKeyConstraints,
			SchemaOpt:                      schemaOpt,
		}, nil
	case *mgmtv1alpha1.JobSourceOptions_Generate:
		if jobSourceConfig.Generate == nil {
			return nil, nil
		}
		return &SqlJobSourceOpts{}, nil
	default:
		return nil, fmt.Errorf("unsupported job source options type for sql job source: %T", jobSourceConfig)
	}
}
