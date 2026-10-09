package runerror

import (
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
)

// sqlstateLength is the length of a SQLSTATE, whose first two characters are its class.
const (
	sqlstateLength      = 5
	sqlstateClassLength = 2
)

// postgresCodes gives the category of the SQLSTATE codes that have one of their own. A code
// listed here is not read by its class.
var postgresCodes = map[string]mgmtv1alpha1.RunErrorCategory{
	// query_canceled (a statement timeout), lock_not_available,
	// idle_in_transaction_session_timeout, idle_session_timeout
	"57014": mgmtv1alpha1.RunErrorCategory_RUN_ERROR_CATEGORY_TIMEOUT,
	"55P03": mgmtv1alpha1.RunErrorCategory_RUN_ERROR_CATEGORY_TIMEOUT,
	"25P03": mgmtv1alpha1.RunErrorCategory_RUN_ERROR_CATEGORY_TIMEOUT,
	"57P05": mgmtv1alpha1.RunErrorCategory_RUN_ERROR_CATEGORY_TIMEOUT,
	// admin_shutdown, crash_shutdown, cannot_connect_now
	"57P01": mgmtv1alpha1.RunErrorCategory_RUN_ERROR_CATEGORY_CONNECTION_REFUSED,
	"57P02": mgmtv1alpha1.RunErrorCategory_RUN_ERROR_CATEGORY_CONNECTION_REFUSED,
	"57P03": mgmtv1alpha1.RunErrorCategory_RUN_ERROR_CATEGORY_CONNECTION_REFUSED,
	// insufficient_privilege
	"42501": mgmtv1alpha1.RunErrorCategory_RUN_ERROR_CATEGORY_INSUFFICIENT_PRIVILEGES,
	// undefined_table, undefined_column, undefined_object, undefined_function,
	// invalid_catalog_name, invalid_schema_name
	"42P01": mgmtv1alpha1.RunErrorCategory_RUN_ERROR_CATEGORY_OBJECT_MISSING,
	"42703": mgmtv1alpha1.RunErrorCategory_RUN_ERROR_CATEGORY_OBJECT_MISSING,
	"42704": mgmtv1alpha1.RunErrorCategory_RUN_ERROR_CATEGORY_OBJECT_MISSING,
	"42883": mgmtv1alpha1.RunErrorCategory_RUN_ERROR_CATEGORY_OBJECT_MISSING,
	"3D000": mgmtv1alpha1.RunErrorCategory_RUN_ERROR_CATEGORY_OBJECT_MISSING,
	"3F000": mgmtv1alpha1.RunErrorCategory_RUN_ERROR_CATEGORY_OBJECT_MISSING,
	// datatype_mismatch, cannot_coerce
	"42804": mgmtv1alpha1.RunErrorCategory_RUN_ERROR_CATEGORY_TYPE_MISMATCH,
	"42846": mgmtv1alpha1.RunErrorCategory_RUN_ERROR_CATEGORY_TYPE_MISMATCH,
}

// postgresClasses gives the category of the SQLSTATE classes that have one, for the codes
// postgresCodes does not list.
var postgresClasses = map[string]mgmtv1alpha1.RunErrorCategory{
	// connection exception
	"08": mgmtv1alpha1.RunErrorCategory_RUN_ERROR_CATEGORY_CONNECTION_REFUSED,
	// invalid authorization specification
	"28": mgmtv1alpha1.RunErrorCategory_RUN_ERROR_CATEGORY_AUTHENTICATION_REFUSED,
	// integrity constraint violation
	"23": mgmtv1alpha1.RunErrorCategory_RUN_ERROR_CATEGORY_CONSTRAINT_VIOLATED,
	// data exception
	"22": mgmtv1alpha1.RunErrorCategory_RUN_ERROR_CATEGORY_TYPE_MISMATCH,
	// insufficient resources
	"53": mgmtv1alpha1.RunErrorCategory_RUN_ERROR_CATEGORY_RESOURCES_EXHAUSTED,
}

// postgresCategory gives the category of a SQLSTATE: that of the code when it has one, else
// that of its class, else "other". What is not five characters long is not a SQLSTATE.
func postgresCategory(sqlstate string) mgmtv1alpha1.RunErrorCategory {
	if len(sqlstate) != sqlstateLength {
		return mgmtv1alpha1.RunErrorCategory_RUN_ERROR_CATEGORY_OTHER
	}
	if category, ok := postgresCodes[sqlstate]; ok {
		return category
	}
	if category, ok := postgresClasses[sqlstate[:sqlstateClassLength]]; ok {
		return category
	}
	return mgmtv1alpha1.RunErrorCategory_RUN_ERROR_CATEGORY_OTHER
}
