// Package native contient les transformers écrits pour le nouveau moteur
// (par opposition aux transformers Benthos existants, enveloppés via l'adaptateur).
//
// DictFaker is the first one: the working BRIDGE between Movements 1 and 2. It
// satisfies the transform.ValueTransformer interface (M1) and draws its determinism
// from the cryptographic consistency module (M2). Two foundations built in
// isolation, composing here into a real transformer — deterministic and consistent
// across databases.
package native

import (
	"fmt"

	"github.com/fishtre-compagnie/husonym/worker/pkg/athanor/consistency"
	"github.com/fishtre-compagnie/husonym/worker/pkg/athanor/transform"
)

// DictFaker remplace une valeur par une entrée d'un dictionnaire, choisie de
// façon déterministe via la graine de cohérence. La même valeur d'entrée, sous
// le même domaine (type sémantique), donne toujours la même sortie — dans toutes
// les bases, tous les runs (RFC §8).
type DictFaker struct {
	domain *consistency.Domain
	dict   []string
}

// NewDictFaker construit un faker sur un domaine de cohérence et un dictionnaire
// (versionné en cible ; changer son contenu change la cohérence historique).
func NewDictFaker(domain *consistency.Domain, dict []string) *DictFaker {
	return &DictFaker{domain: domain, dict: dict}
}

func (f *DictFaker) TransformValue(_ transform.Ctx, in any) (any, error) {
	if len(f.dict) == 0 {
		return nil, fmt.Errorf("native: dictionnaire vide")
	}
	if in == nil {
		return nil, nil // on ne remplace pas un NULL
	}
	seed := f.domain.Seed(fmt.Sprint(in))
	return f.dict[seed.Index(len(f.dict))], nil
}

var _ transform.ValueTransformer = (*DictFaker)(nil)
