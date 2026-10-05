package sqlmanager_mssql

import "github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager/mssql/ddl"

// Block labels. SchemasLabel and CreateTablesLabel are those of sqlmanager_shared.
const (
	DataTypesLabel       = ddl.DataTypesLabel
	ViewsFunctionsLabel  = ddl.ViewsFunctionsLabel
	NonFkAlterTableLabel = ddl.NonFkAlterTableLabel
	TableIndexLabel      = ddl.TableIndexLabel
	FkAlterTableLabel    = ddl.FkAlterTableLabel
	TableTriggersLabel   = ddl.TableTriggersLabel
)
