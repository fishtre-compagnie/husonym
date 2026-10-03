package ddl

import (
	"strings"
)

// moduleKinds names, for the message of a failed creation, what each type code is.
var moduleKinds = map[string]string{
	TypeView:           "view",
	TypeScalarFunction: "function",
	TypeInlineFunction: "function",
	TypeTableFunction:  "function",
	TypeProcedure:      "procedure",
	TypeTrigger:        "trigger",
}

func setting(on bool) string {
	if on {
		return "ON"
	}
	return "OFF"
}

// createModule writes a view, a function, a procedure or a trigger from its stored definition.
//
// The definition runs inside two levels of EXEC. The outer one sets the two session options the
// module was created under, which end with it: nothing is left on the connection. The inner one
// makes CREATE the first statement of its batch. The definition holds the name the module was
// created with, which a rename does not change: the statement fails when running it did not
// create the object under the name the catalog gives.
func createModule(module *Module) string {
	return createGuarded(module, objectMissing(module.Schema, module.Name, module.Type))
}

// createTrigger writes a trigger behind the test of its name on its parent: a trigger of that
// name on another table or view is not this one, and the server refuses to create a second.
func createTrigger(module *Module, parent string) string {
	return createGuarded(module, triggerMissing(module.Schema, parent, module.Name))
}

// createGuarded runs the definition of a module when the test missing holds, and fails when it
// still holds afterwards.
func createGuarded(module *Module, missing string) string {
	name := QualifiedName(module.Schema, module.Name)
	batch := "SET ANSI_NULLS " + setting(module.UsesAnsiNulls) +
		"; SET QUOTED_IDENTIFIER " + setting(module.UsesQuotedIdentifier) +
		"; EXEC (" + QuoteLiteral(module.Definition) + ")"
	// THROW reads its message as a format: a percent sign stands for itself when doubled.
	message := strings.ReplaceAll(
		"the definition of "+moduleKinds[module.Type]+" "+name+" did not create it under that name",
		"%", "%%",
	)
	return missing + "\n" +
		"BEGIN\n" +
		"    EXEC (" + QuoteLiteral(batch) + ");\n" +
		"    " + missing + "\n" +
		"        THROW 50000, " + QuoteLiteral(message) + ", 1;\n" +
		"END"
}

// disableTrigger disables a trigger of a table or of a view for as long as it is enabled.
func disableTrigger(schema, parent, name string) string {
	return guarded(
		triggerEnabled(schema, parent, name),
		"DISABLE TRIGGER "+QualifiedName(schema, name)+" ON "+QualifiedName(schema, parent),
	)
}
