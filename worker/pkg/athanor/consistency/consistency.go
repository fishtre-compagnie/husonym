// Package consistency implémente la cohérence d'anonymisation déterministe SANS
// ÉTAT (RFC §8) : « la même donnée d'entrée produit toujours la même donnée de
// sortie, dans tous les SGBD, tous les runs » — obtenu par dérivation
// cryptographique, sans base de correspondance à administrer.
//
// La hiérarchie de dérivation (HMAC-SHA256 à chaque étage) :
//
//	cléProjet  (secret, fourni par le Key Service)
//	  └─ cléScope   = HMAC(cléProjet, "scope:"+scope)          scope = org | project:… | run:…
//	       └─ cléDomaine = HMAC(cléScope, "domain:"+typeSémantique)
//	            └─ graine(valeur) = HMAC(cléDomaine, canonicalize(valeur))
//
// DÉCISION STRUCTURANTE : la clé de domaine est dérivée du TYPE SÉMANTIQUE
// (person.email, geo.postal_code…), PAS du nom de colonne. Conséquence : deux
// colonnes de bases différentes (pg.users.email et mysql.contacts.mail), toutes
// deux classées person.email, produisent la MÊME graine pour la MÊME valeur —
// sans qu'aucun des deux traitements ne connaisse l'autre. La cohérence
// inter-bases, inter-runs et batch/stream en découle gratuitement.
//
// Ce package est volontairement pur (stdlib crypto uniquement), sans dépendance
// au reste du moteur : c'est la brique la plus isolée et la plus testable.
package consistency

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"strings"
)

// Deriver porte la clé de scope dérivée de la clé de projet. On le construit une
// fois par run (ou par processus), puis on en tire des domaines.
type Deriver struct {
	scopeKey []byte
}

// New construit un Deriver à partir de la clé de projet (le secret, fourni par le
// Key Service — jamais journalisé) et d'un scope de cohérence.
//
// Le scope définit la portée de l'égalité déterministe :
//   - "org"          : cohérence maximale (même valeur → même sortie dans toute l'organisation)
//   - "project:<id>" : cohérence limitée à un projet
//   - "run:<id>"     : graine éphémère (jeux de données jetables, cohérence non désirée entre runs)
func New(projectKey []byte, scope string) *Deriver {
	return &Deriver{scopeKey: mac(projectKey, "scope:"+scope)}
}

// Domain renvoie le contexte de dérivation pour un type sémantique donné.
// C'est ICI que se joue la cohérence inter-bases : deux colonnes partageant le
// même typeSémantique partagent la même clé de domaine.
func (d *Deriver) Domain(semanticType string) *Domain {
	return &Domain{
		key:   mac(d.scopeKey, "domain:"+semanticType),
		canon: DefaultCanonicalizer,
	}
}

// Domain porte la clé d'un type sémantique et sa politique de canonicalisation.
type Domain struct {
	key   []byte
	canon Canonicalizer
}

// WithCanonicalizer permet de surcharger la normalisation d'entrée pour ce
// domaine (ex. conserver la casse pour un identifiant sensible à la casse).
func (dom *Domain) WithCanonicalizer(c Canonicalizer) *Domain {
	return &Domain{key: dom.key, canon: c}
}

// Seed dérive la graine déterministe d'une valeur. Deux valeurs qui se
// canonicalisent identiquement produisent la même graine.
func (dom *Domain) Seed(value string) Seed {
	return Seed(sha256hmac(dom.key, dom.canon(value)))
}

// Seed est le condensé déterministe (32 octets) d'une valeur dans un domaine.
// Il alimente les générateurs : index de dictionnaire, entier borné, graine de
// PRNG pour le bruit (Noise Engine), matériel de clé pour le FPE (à venir).
type Seed [32]byte

// Uint64 renvoie les 8 premiers octets de la graine en entier non signé.
func (s Seed) Uint64() uint64 {
	return binary.BigEndian.Uint64(s[:8])
}

// Index renvoie un indice déterministe dans [0, n) — pour choisir dans un
// dictionnaire faker versionné. Panique si n <= 0.
//
// Note : un simple modulo introduit un biais statistique négligeable tant que n
// est très petit devant 2^64 (cas des dictionnaires). Acceptable pour de
// l'anonymisation ; à revoir si une uniformité stricte devient nécessaire.
func (s Seed) Index(n int) int {
	if n <= 0 {
		panic("consistency: Index requiert n > 0")
	}
	return int(s.Uint64() % uint64(n))
}

// IntInRange renvoie un entier déterministe dans [min, max] (bornes incluses).
// Si min > max, les bornes sont échangées.
func (s Seed) IntInRange(min, max int64) int64 {
	if min > max {
		min, max = max, min
	}
	span := uint64(max-min) + 1
	return min + int64(s.Uint64()%span)
}

// Float01 renvoie un flottant déterministe dans [0, 1) — pratique pour seeder un
// générateur de bruit.
func (s Seed) Float01() float64 {
	// 53 bits de mantisse pour un float64 uniforme dans [0,1).
	return float64(s.Uint64()>>11) / (1 << 53)
}

// Bytes expose la graine complète (pour le FPE, futurs usages).
func (s Seed) Bytes() []byte {
	out := make([]byte, len(s))
	copy(out, s[:])
	return out
}

// Canonicalizer normalise une valeur d'entrée avant dérivation, pour que des
// variantes équivalentes convergent vers la même graine.
type Canonicalizer func(string) string

// DefaultCanonicalizer : espaces de bordure supprimés + minuscules (Unicode).
// La suppression des accents pourra être ajoutée via une option de domaine.
func DefaultCanonicalizer(v string) string {
	return strings.ToLower(strings.TrimSpace(v))
}

// PreserveCase : normalisation minimale (espaces de bordure uniquement).
func PreserveCase(v string) string {
	return strings.TrimSpace(v)
}

// --- primitives HMAC ---

func sha256hmac(key []byte, msg string) [32]byte {
	h := hmac.New(sha256.New, key)
	h.Write([]byte(msg))
	var out [32]byte
	copy(out[:], h.Sum(nil))
	return out
}

func mac(key []byte, msg string) []byte {
	d := sha256hmac(key, msg)
	return d[:]
}
