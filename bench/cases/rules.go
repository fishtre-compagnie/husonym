package cases

import "github.com/fishtre-compagnie/husonym/bench/schema"

// ruleCases are the rules users write themselves, in JavaScript: what a script may reach,
// how long it may run, and what it may keep from one row to the next.
func ruleCases() []*Case {
	return []*Case{
		transformerFailure("js-endless-script",
			"Script sans fin : le run échoue au bout de la limite de temps, au lieu de bloquer la table",
			"libelle", transformJavascript(`while (true) {}`), sameRunError("ran past its time limit")),
		// Before the guard, require() read the file and ran it as JavaScript: a host name
		// came back in the error message.
		transformerFailure("js-reads-host-file",
			"Script qui charge un fichier du worker : refusé, rien n'est lu",
			"libelle", transformJavascript(`return String(require("/etc/hostname"));`),
			sameRunError("scripts cannot load files")),
	}
}

// sameRunError expects the same message on every database: an error of the engine, not
// one the database prints in its own words.
func sameRunError(message string) map[schema.Dialect]string {
	return map[schema.Dialect]string{schema.MySQL: message, schema.Postgres: message}
}
