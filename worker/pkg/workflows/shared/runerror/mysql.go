package runerror

import (
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
)

// mysqlNumbers gives the category of the error numbers of MySQL that have one.
var mysqlNumbers = map[uint16]mgmtv1alpha1.RunErrorCategory{
	// server shutdown in progress, host blocked, host not allowed to connect
	1053: mgmtv1alpha1.RunErrorCategory_RUN_ERROR_CATEGORY_CONNECTION_REFUSED,
	1129: mgmtv1alpha1.RunErrorCategory_RUN_ERROR_CATEGORY_CONNECTION_REFUSED,
	1130: mgmtv1alpha1.RunErrorCategory_RUN_ERROR_CATEGORY_CONNECTION_REFUSED,
	// access denied for the user, with or without a password, expired password, locked
	// account
	1045: mgmtv1alpha1.RunErrorCategory_RUN_ERROR_CATEGORY_AUTHENTICATION_REFUSED,
	1698: mgmtv1alpha1.RunErrorCategory_RUN_ERROR_CATEGORY_AUTHENTICATION_REFUSED,
	1862: mgmtv1alpha1.RunErrorCategory_RUN_ERROR_CATEGORY_AUTHENTICATION_REFUSED,
	3118: mgmtv1alpha1.RunErrorCategory_RUN_ERROR_CATEGORY_AUTHENTICATION_REFUSED,
	// access denied to a database, a table, a column, a privileged statement, a routine; a
	// server that only reads (two forms)
	1044: mgmtv1alpha1.RunErrorCategory_RUN_ERROR_CATEGORY_INSUFFICIENT_PRIVILEGES,
	1142: mgmtv1alpha1.RunErrorCategory_RUN_ERROR_CATEGORY_INSUFFICIENT_PRIVILEGES,
	1143: mgmtv1alpha1.RunErrorCategory_RUN_ERROR_CATEGORY_INSUFFICIENT_PRIVILEGES,
	1227: mgmtv1alpha1.RunErrorCategory_RUN_ERROR_CATEGORY_INSUFFICIENT_PRIVILEGES,
	1370: mgmtv1alpha1.RunErrorCategory_RUN_ERROR_CATEGORY_INSUFFICIENT_PRIVILEGES,
	1290: mgmtv1alpha1.RunErrorCategory_RUN_ERROR_CATEGORY_INSUFFICIENT_PRIVILEGES,
	1836: mgmtv1alpha1.RunErrorCategory_RUN_ERROR_CATEGORY_INSUFFICIENT_PRIVILEGES,
	// lock wait timeout, maximum statement execution time, client disconnected for its
	// inactivity, network read and write timeouts
	1205: mgmtv1alpha1.RunErrorCategory_RUN_ERROR_CATEGORY_TIMEOUT,
	3024: mgmtv1alpha1.RunErrorCategory_RUN_ERROR_CATEGORY_TIMEOUT,
	4031: mgmtv1alpha1.RunErrorCategory_RUN_ERROR_CATEGORY_TIMEOUT,
	1159: mgmtv1alpha1.RunErrorCategory_RUN_ERROR_CATEGORY_TIMEOUT,
	1161: mgmtv1alpha1.RunErrorCategory_RUN_ERROR_CATEGORY_TIMEOUT,
	// column cannot be null, duplicate entry (three forms), foreign key (four forms), no
	// default value, check constraint, duplicate key (three forms)
	1022: mgmtv1alpha1.RunErrorCategory_RUN_ERROR_CATEGORY_CONSTRAINT_VIOLATED,
	1761: mgmtv1alpha1.RunErrorCategory_RUN_ERROR_CATEGORY_CONSTRAINT_VIOLATED,
	1762: mgmtv1alpha1.RunErrorCategory_RUN_ERROR_CATEGORY_CONSTRAINT_VIOLATED,
	1048: mgmtv1alpha1.RunErrorCategory_RUN_ERROR_CATEGORY_CONSTRAINT_VIOLATED,
	1062: mgmtv1alpha1.RunErrorCategory_RUN_ERROR_CATEGORY_CONSTRAINT_VIOLATED,
	1169: mgmtv1alpha1.RunErrorCategory_RUN_ERROR_CATEGORY_CONSTRAINT_VIOLATED,
	1216: mgmtv1alpha1.RunErrorCategory_RUN_ERROR_CATEGORY_CONSTRAINT_VIOLATED,
	1217: mgmtv1alpha1.RunErrorCategory_RUN_ERROR_CATEGORY_CONSTRAINT_VIOLATED,
	1364: mgmtv1alpha1.RunErrorCategory_RUN_ERROR_CATEGORY_CONSTRAINT_VIOLATED,
	1451: mgmtv1alpha1.RunErrorCategory_RUN_ERROR_CATEGORY_CONSTRAINT_VIOLATED,
	1452: mgmtv1alpha1.RunErrorCategory_RUN_ERROR_CATEGORY_CONSTRAINT_VIOLATED,
	1586: mgmtv1alpha1.RunErrorCategory_RUN_ERROR_CATEGORY_CONSTRAINT_VIOLATED,
	3819: mgmtv1alpha1.RunErrorCategory_RUN_ERROR_CATEGORY_CONSTRAINT_VIOLATED,
	// unknown database, table (two forms), column, routine
	1049: mgmtv1alpha1.RunErrorCategory_RUN_ERROR_CATEGORY_OBJECT_MISSING,
	1051: mgmtv1alpha1.RunErrorCategory_RUN_ERROR_CATEGORY_OBJECT_MISSING,
	1054: mgmtv1alpha1.RunErrorCategory_RUN_ERROR_CATEGORY_OBJECT_MISSING,
	1146: mgmtv1alpha1.RunErrorCategory_RUN_ERROR_CATEGORY_OBJECT_MISSING,
	1305: mgmtv1alpha1.RunErrorCategory_RUN_ERROR_CATEGORY_OBJECT_MISSING,
	// value out of range (two forms), truncated, incorrect for its type, too long, invalid JSON
	1690: mgmtv1alpha1.RunErrorCategory_RUN_ERROR_CATEGORY_TYPE_MISMATCH,
	1264: mgmtv1alpha1.RunErrorCategory_RUN_ERROR_CATEGORY_TYPE_MISMATCH,
	1265: mgmtv1alpha1.RunErrorCategory_RUN_ERROR_CATEGORY_TYPE_MISMATCH,
	1292: mgmtv1alpha1.RunErrorCategory_RUN_ERROR_CATEGORY_TYPE_MISMATCH,
	1366: mgmtv1alpha1.RunErrorCategory_RUN_ERROR_CATEGORY_TYPE_MISMATCH,
	1406: mgmtv1alpha1.RunErrorCategory_RUN_ERROR_CATEGORY_TYPE_MISMATCH,
	3140: mgmtv1alpha1.RunErrorCategory_RUN_ERROR_CATEGORY_TYPE_MISMATCH,
	// disk full, out of memory (two forms), too many connections, out of resources, table
	// full, too many connections of the user, resource limit of the user, lock table full
	1206: mgmtv1alpha1.RunErrorCategory_RUN_ERROR_CATEGORY_RESOURCES_EXHAUSTED,
	1021: mgmtv1alpha1.RunErrorCategory_RUN_ERROR_CATEGORY_RESOURCES_EXHAUSTED,
	1037: mgmtv1alpha1.RunErrorCategory_RUN_ERROR_CATEGORY_RESOURCES_EXHAUSTED,
	1038: mgmtv1alpha1.RunErrorCategory_RUN_ERROR_CATEGORY_RESOURCES_EXHAUSTED,
	1040: mgmtv1alpha1.RunErrorCategory_RUN_ERROR_CATEGORY_RESOURCES_EXHAUSTED,
	1041: mgmtv1alpha1.RunErrorCategory_RUN_ERROR_CATEGORY_RESOURCES_EXHAUSTED,
	1114: mgmtv1alpha1.RunErrorCategory_RUN_ERROR_CATEGORY_RESOURCES_EXHAUSTED,
	1203: mgmtv1alpha1.RunErrorCategory_RUN_ERROR_CATEGORY_RESOURCES_EXHAUSTED,
	1226: mgmtv1alpha1.RunErrorCategory_RUN_ERROR_CATEGORY_RESOURCES_EXHAUSTED,
}

// mysqlCategory gives the category of an error number of MySQL, "other" when it has none.
func mysqlCategory(number uint16) mgmtv1alpha1.RunErrorCategory {
	if category, ok := mysqlNumbers[number]; ok {
		return category
	}
	return mgmtv1alpha1.RunErrorCategory_RUN_ERROR_CATEGORY_OTHER
}
