// Package consistency implements STATELESS deterministic anonymization
// consistency (RFC §8): "the same input always produces the same output, across
// every database and every run" — obtained by cryptographic derivation, with no
// mapping store to operate.
//
// La hiérarchie de dérivation (HMAC-SHA256 à chaque étage) :
//
//	cléProjet  (secret, fourni par le Key Service)
//	  └─ cléScope   = HMAC(cléProjet, "scope:"+scope)          scope = run:… | job:… | account:…
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
	"math"
	"strings"
)

// Deriver porte la clé de scope dérivée de la clé de projet. On le construit une
// fois par run (ou par processus), puis on en tire des domaines.
type Deriver struct {
	scopeKey []byte
}

// New builds a Deriver from the project key (the secret, provided by the Key
// Service — never written to logs) and a consistency scope.
//
// Le scope définit la portée de l'égalité déterministe :
//   - "run:<id>"     : cohérence limitée à un run (sorties non reliables d'un run à l'autre)
//   - "job:<id>"     : cohérence entre les runs d'un même job
//   - "account:<id>" : cohérence dans tout le compte (pseudonymisation au sens du RGPD)
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

// CipherKey returns the key of a keyed permutation (FPE) for a semantic type, in the
// same scope as its domain. It is derived under its own label, never from the domain
// key: a value is MACed under the domain key, and a value spelling the label must not
// yield the cipher key.
func (d *Deriver) CipherKey(semanticType string) [32]byte {
	return sha256hmac(d.scopeKey, "cipher:"+semanticType)
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
	// Le reste est strictement inférieur à n, donc tient dans un int par
	// construction : la conversion ne peut pas déborder.
	return int(s.Uint64() % uint64(n)) //nolint:gosec // reste < n, borné par un int
}

// IntInRange renvoie un entier déterministe dans [lo, hi] (bornes incluses).
// Si lo > hi, les bornes sont échangées.
//
// La largeur se calcule en arithmétique non signée : `hi - lo` déborde dès que la
// plage couvre plus de la moitié de l'espace int64, et le calcul naïf donnait
// alors une largeur nulle, donc un modulo par zéro — panique sur [MinInt64,
// MaxInt64].
func (s Seed) IntInRange(lo, hi int64) int64 {
	if lo > hi {
		lo, hi = hi, lo
	}
	//nolint:gosec // réinterprétation non signée voulue : l'écart reste exact
	span := uint64(hi) - uint64(lo) // exact même quand hi-lo déborde en signé
	if span == math.MaxUint64 {
		// Plage int64 entière : toute valeur convient, et span+1 déborderait.
		return int64(s.Uint64()) //nolint:gosec // toute la plage int64 est valide
	}
	offset := s.Uint64() % (span + 1) // offset <= span, donc lo+offset <= hi
	return lo + int64(offset)         //nolint:gosec // repli modulaire voulu, résultat dans [lo, hi]
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

// Canonicalizer normalizes an input value before derivation, so that equivalent
// spellings converge on the same seed.
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

// Exact leaves the value as it is: two values derive the same seed only when they are
// equal. It is the choice for the domains of a user's rule, which says itself what
// counts as the same value.
func Exact(v string) string {
	return v
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
