// Package javascript_draft builds the prompt that gets a javascript transformer drafted for a
// column, by an agent or by a model the server calls itself.
//
// It lives next to javascript_userland because it describes that package's contract — the
// signature the code is a body of, what is in scope, what a return means. A prompt that
// describes a contract belongs beside it: when the wrapper changes, the odds that someone
// updates the description are the odds that they look in the same directory.
//
// What it is for. A model asked to "anonymize a column" writes plausible javascript that
// fails on this platform for reasons it cannot guess: it hashes with a crypto API that goja
// does not have, it returns a constant into a unique index, it regenerates a foreign key that
// no longer matches its parent, it writes 30 characters into a varchar(20). None of that is a
// weakness of the model — it is missing facts, and every one of those facts is one this server
// holds. The prompt is where they are handed over.
package javascript_draft

import (
	"fmt"
	"strings"

	pseudo_functions "github.com/fishtre-compagnie/husonym/internal/javascript/functions/pseudo"
)

// Mode is the kind of transformer being drafted.
type Mode string

const (
	// ModeTransform derives the new value from the old one: the body of fn(value, input).
	ModeTransform Mode = "transform"
	// ModeGenerate ignores the old value: the body of fn().
	ModeGenerate Mode = "generate"
)

// Engine is the engine that will run the rule. It decides whether the deterministic pseudo
// functions are in scope, which changes what can be drafted rather than how well: without
// them there is no way to keep a foreign key pointing at its parent.
type Engine string

const (
	// EngineAthanor offers the pseudo namespace.
	EngineAthanor Engine = "athanor"
	// EngineBenthos does not: it has no consistency scope, and a call there fails.
	EngineBenthos Engine = "benthos"
)

// ForeignKey is the parent a column points at.
type ForeignKey struct {
	Schema string
	Table  string
	Column string
}

// ColumnFacts is what the server knows about the column, and the model does not. Every field
// is optional except the identity of the column itself: an unknown fact is left out of the
// prompt rather than asserted, because a model told "not unique" about a column nobody
// checked will happily return a constant.
type ColumnFacts struct {
	Schema string
	Table  string
	Column string

	// DataType is the column type as the source reports it, e.g. "character varying(20)".
	DataType string
	// MaxLength bounds the output, when the type carries a length.
	MaxLength *int32
	// IsNullable tells whether null is an acceptable output.
	IsNullable bool
	// IsUnique is true when a unique index or primary key covers this column alone.
	IsUnique bool
	// IsGenerated and IsIdentity mark columns the database writes itself.
	IsGenerated bool
	IsIdentity  bool
	// ForeignKey is the parent this column points at, when it has one.
	ForeignKey *ForeignKey
	// ReferencedBy counts the foreign keys pointing at this column.
	ReferencedBy int32

	// PiiCategory is the detected category, when a report covered the column.
	PiiCategory string

	Mode   Mode
	Engine Engine
}

// BuildPrompt renders the prompt. It is deterministic: the same facts give the same text, so a
// draft can be reproduced and a change in the prompt shows up in a diff rather than in a
// model's mood.
func BuildPrompt(facts *ColumnFacts) string {
	var b strings.Builder

	b.WriteString("Write the body of a JavaScript function that anonymizes one database column.\n\n")

	b.WriteString("## The contract\n\n")
	writeContract(&b, facts)

	b.WriteString("\n## The column\n\n")
	writeColumn(&b, facts)

	b.WriteString("\n## Rules you must satisfy\n\n")
	writeRules(&b, facts)

	b.WriteString("\n## Available functions\n\n")
	writeFunctions(&b, facts)

	b.WriteString("\n## Answer with\n\n")
	b.WriteString("The function body only: no function declaration, no markdown fence, no commentary.\n")

	return b.String()
}

func writeContract(b *strings.Builder, facts *ColumnFacts) {
	if facts.Mode == ModeGenerate {
		b.WriteString("Your code is the body of `function fn(){ ... }`. It takes no arguments and must `return` the value to write.\n")
	} else {
		b.WriteString("Your code is the body of `function fn(value, input){ ... }` and must `return` the value to write.\n")
		b.WriteString(
			"`value` is the column's current value. `input` is the whole source row, keyed by column name, so a rule may read its siblings — that is how a value is derived from a key rather than invented.\n",
		)
	}
	b.WriteString(
		"\nIt runs in goja, an embedded ECMAScript 5.1+ engine. There is no Node, no browser, no `require`, no `import`, no network, no filesystem, no `crypto`, no `Buffer`, no timers. Only the standard library and the functions listed below exist.\n",
	)
	b.WriteString(
		"\nAssigning to an undeclared variable, or to a property of `husonym`, `globalThis` or `this`, shares state with the other columns of the same row and is cleared at the next row. Use it only when columns must agree with each other; never to carry state from one row to the next, which would make the job's output depend on the order rows are read.\n",
	)
	b.WriteString("\nIntegers beyond 2^53 arrive as BigInt, so arithmetic on an id may need BigInt literals.\n")
}

func writeColumn(b *strings.Builder, facts *ColumnFacts) {
	fmt.Fprintf(b, "- Column: `%s.%s.%s`\n", facts.Schema, facts.Table, facts.Column)
	if facts.DataType != "" {
		fmt.Fprintf(b, "- Type: `%s`\n", facts.DataType)
	}
	if facts.MaxLength != nil {
		fmt.Fprintf(b, "- Maximum length: %d characters\n", *facts.MaxLength)
	}
	if facts.IsNullable {
		b.WriteString("- Accepts null\n")
	} else {
		b.WriteString("- Rejects null\n")
	}
	if facts.IsUnique {
		b.WriteString("- Under a uniqueness constraint\n")
	}
	if facts.ForeignKey != nil {
		fmt.Fprintf(
			b,
			"- Foreign key onto `%s.%s.%s`\n",
			facts.ForeignKey.Schema, facts.ForeignKey.Table, facts.ForeignKey.Column,
		)
	}
	if facts.ReferencedBy > 0 {
		fmt.Fprintf(b, "- Referenced by %d foreign key(s)\n", facts.ReferencedBy)
	}
	if facts.IsGenerated {
		b.WriteString("- Generated by the database\n")
	}
	if facts.IsIdentity {
		b.WriteString("- Identity column\n")
	}
	if facts.PiiCategory != "" {
		fmt.Fprintf(b, "- Detected as personal data, category `%s`\n", facts.PiiCategory)
	}
	// Said out loud, because a model that does not know the sample is withheld asks for it, or
	// invents one and reasons from the invention.
	b.WriteString(
		"\nYou are not shown any of the column's values, and you do not need them: write a rule that holds for the whole column.\n",
	)
}

// writeRules turns the facts into imperatives. The facts alone are not enough: a model reading
// "under a uniqueness constraint" still has to work out that a constant breaks on the second
// row, and the whole point of the exercise is that it should not have to.
func writeRules(b *strings.Builder, facts *ColumnFacts) {
	rules := []string{}

	if facts.IsGenerated || facts.IsIdentity {
		rules = append(
			rules,
			"The database writes this column itself. Do not draft a rule for it — say so instead of returning a value.",
		)
	}

	// Uniqueness and foreign keys are satisfied by deriving the output from the input, and a
	// generate rule is handed no input: `fn()` takes no arguments. Emitting those rules here
	// would ask for something the contract forbids, and the draft that came back would say
	// `value` and throw ReferenceError on the first row. So the demand is refused, once,
	// instead of being restated in terms the mode cannot meet.
	if facts.Mode == ModeGenerate &&
		(facts.IsUnique || facts.ForeignKey != nil || facts.ReferencedBy > 0) {
		rules = append(
			rules,
			"This column must agree with something else — it is unique, or it is part of a foreign key — which can only be done by deriving the output from the current value. A generate rule does not receive it. Do not draft one: say that this column needs a transform rule instead.",
		)
		for _, rule := range rules {
			fmt.Fprintf(b, "- %s\n", rule)
		}
		return
	}

	if facts.IsUnique {
		rules = append(
			rules,
			"The output must be unique across every row. Derive it from the input so that two different inputs give two different outputs — never a constant, and never a random draw, which collides.",
		)
	}
	if facts.ForeignKey != nil {
		rules = append(
			rules,
			fmt.Sprintf(
				"This column points at `%s.%s.%s`. Its output must be the same as the output of that parent column for the same original value, or the row loses its parent. Only a deterministic function of the input can do this.",
				facts.ForeignKey.Schema,
				facts.ForeignKey.Table,
				facts.ForeignKey.Column,
			),
		)
	}
	if facts.ReferencedBy > 0 {
		rules = append(
			rules,
			"Other tables point at this column, so the same input must always give the same output, in every table and every run.",
		)
	}
	if !facts.IsNullable {
		rules = append(rules, "Never return null or undefined.")
	} else if facts.Mode == ModeTransform {
		rules = append(
			rules,
			"A null input stays null: return it unchanged rather than anonymizing the absence of a value.",
		)
	}
	if facts.MaxLength != nil {
		rules = append(
			rules,
			fmt.Sprintf(
				"The output must be at most %d characters. Bound it; do not assume the generated value is short.",
				*facts.MaxLength,
			),
		)
	}
	rules = append(
		rules,
		"Keep the format the column's type implies unless a rule here forbids it: a value the application can still parse is what makes the anonymized database usable.",
	)
	if facts.Engine == EngineBenthos {
		rules = append(
			rules,
			"This job runs on Benthos, which has no consistency scope: the `pseudo` functions are not available and calling one fails at runtime.",
		)
	}

	rules = append(
		rules,
		"Do not invent a hash: there is no crypto API. Determinism comes from the functions below.",
	)

	for _, rule := range rules {
		fmt.Fprintf(b, "- %s\n", rule)
	}
}

// writeFunctions derives the catalogue from the package that defines it. Copying the list here
// would leave the prompt describing a namespace that has moved on, silently, and a model cannot
// tell a function that never existed from one that was removed.
func writeFunctions(b *strings.Builder, facts *ColumnFacts) {
	if facts.Engine != EngineAthanor {
		b.WriteString("Standard ECMAScript only. The `pseudo` namespace is not available on this engine.\n")
		return
	}

	b.WriteString(
		"The `pseudo` namespace is deterministic: the same input gives the same output on every row, table and run of the job's consistency scope. This is what keeps foreign keys pointing at their parents.\n\n",
	)
	fmt.Fprintf(
		b,
		"- `pseudo.<kind>(value)` returns a fake of that kind, for: %s.\n",
		strings.Join(pseudo_functions.Kinds, ", "),
	)
	b.WriteString("- `pseudo.hash(value, domain)` returns a hexadecimal digest of the value.\n")
	b.WriteString("- `pseudo.int(value, domain, min, max)` returns an integer within the range.\n")
	b.WriteString("- `pseudo.pick(list, value, domain)` returns one element of the list.\n")
	b.WriteString(
		"\n`domain` is a label of your choosing. Two calls with the same domain and the same value agree; two different domains give unrelated results for the same value. Use one domain per meaning — the same one on a foreign key and on the parent it points at, a different one for unrelated columns.\n",
	)
	b.WriteString("\nA null value returns null from every one of these, so a null input needs no special case.\n")
}
