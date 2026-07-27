package native

// format.go — transformers déterministes pour les formats « quasi-uniques »
// (email, téléphone). Contrairement à DictFaker (qui choisit dans un petit
// dictionnaire, donc regroupe les valeurs), on DÉRIVE la sortie de la graine de
// cohérence (RFC §8) : même entrée → même sortie, et des entrées distinctes
// donnent des sorties distinctes (statistiquement) — ce qui préserve les
// jointures/déduplications quand ces colonnes servent de référence.
//
// NB : ce n'est PAS encore du FPE (FF3-1, RFC §8.2). Le FPE préserverait le
// format/longueur exacts et serait réversible ; ici on produit une valeur valide
// et déterministe, suffisante pour l'anonymisation. Le FPE reste un raffinement.

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"

	"github.com/Groupe-Hevea/neosync/worker/pkg/athanor/consistency"
	"github.com/Groupe-Hevea/neosync/worker/pkg/athanor/transform"
)

// EmailFaker produit un email déterministe : partie locale dérivée de la graine
// (quasi-unique) + domaine choisi déterministement dans une liste versionnée.
type EmailFaker struct {
	domain  *consistency.Domain
	domains []string
}

// NewEmailFaker construit un faker d'email sur un domaine de cohérence et une
// liste de domaines d'email (ex. data-sets EmailDomains).
func NewEmailFaker(domain *consistency.Domain, emailDomains []string) *EmailFaker {
	return &EmailFaker{domain: domain, domains: emailDomains}
}

func (f *EmailFaker) TransformValue(_ transform.Ctx, in any) (any, error) {
	if in == nil {
		return nil, nil
	}
	if len(f.domains) == 0 {
		return nil, fmt.Errorf("native: aucun domaine d'email fourni")
	}
	seed := f.domain.Seed(fmt.Sprint(in))
	local := hex.EncodeToString(seed.Bytes()[:16]) // 32 hexa, quasi-unique
	return local + "@" + f.domains[seed.Index(len(f.domains))], nil
}

var _ transform.ValueTransformer = (*EmailFaker)(nil)

// PhoneFaker produit un numéro de mobile français déterministe : 0[67] suivi de
// 8 chiffres dérivés de la graine.
type PhoneFaker struct {
	domain *consistency.Domain
}

// NewPhoneFaker construit un faker de téléphone sur un domaine de cohérence.
func NewPhoneFaker(domain *consistency.Domain) *PhoneFaker {
	return &PhoneFaker{domain: domain}
}

func (f *PhoneFaker) TransformValue(_ transform.Ctx, in any) (any, error) {
	if in == nil {
		return nil, nil
	}
	seed := f.domain.Seed(fmt.Sprint(in))
	u := seed.Uint64()
	prefix := 6 + int(u%2)         // 06 ou 07
	digits := (u >> 1) % 100000000 // 8 chiffres
	return fmt.Sprintf("0%d%08d", prefix, digits), nil
}

var _ transform.ValueTransformer = (*PhoneFaker)(nil)

// FullNameFaker compose un nom complet déterministe « Prénom Nom » à partir de
// deux dictionnaires. Les deux indices sont dérivés de moitiés distinctes de la
// graine, pour que prénom et nom varient indépendamment tout en restant
// déterministes pour une même entrée.
type FullNameFaker struct {
	domain      *consistency.Domain
	first, last []string
}

// NewFullNameFaker construit un faker de nom complet sur un domaine de cohérence
// et les dictionnaires de prénoms/noms.
func NewFullNameFaker(domain *consistency.Domain, first, last []string) *FullNameFaker {
	return &FullNameFaker{domain: domain, first: first, last: last}
}

func (f *FullNameFaker) TransformValue(_ transform.Ctx, in any) (any, error) {
	if in == nil {
		return nil, nil
	}
	if len(f.first) == 0 || len(f.last) == 0 {
		return nil, fmt.Errorf("native: dictionnaire prénom/nom vide")
	}
	b := f.domain.Seed(fmt.Sprint(in)).Bytes()
	fi := binary.BigEndian.Uint64(b[0:8]) % uint64(len(f.first))
	li := binary.BigEndian.Uint64(b[8:16]) % uint64(len(f.last))
	return f.first[fi] + " " + f.last[li], nil
}

var _ transform.ValueTransformer = (*FullNameFaker)(nil)
