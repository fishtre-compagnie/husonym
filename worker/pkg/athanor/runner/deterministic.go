package runner

// deterministic.go — branche la COHÉRENCE DÉTERMINISTE (RFC §8) sur le format de
// job Neosync. C'est le câblage qui manquait : jusqu'ici SpecForTable n'utilisait
// que les transformers Benthos ALÉATOIRES ; ici, pour les types qu'on sait mapper
// vers un dictionnaire faker, on instancie un native.DictFaker adossé à un domaine
// de cohérence. Résultat : la même valeur d'entrée produit TOUJOURS la même sortie
// — sur toutes les lignes, toutes les tables, tous les runs (même clé/scope).
//
// La clé de dérivation est le TYPE SÉMANTIQUE (person.first_name…), pas le nom de
// colonne : deux colonnes de même type sémantique convergent, y compris entre
// bases (RFC §8.1). Les dictionnaires sont ceux de Neosync (versionnés : changer
// leur contenu change la cohérence historique).

import (
	mgmtv1alpha1 "github.com/Groupe-Hevea/neosync/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/Groupe-Hevea/neosync/worker/pkg/athanor/consistency"
	"github.com/Groupe-Hevea/neosync/worker/pkg/athanor/native"
	"github.com/Groupe-Hevea/neosync/worker/pkg/athanor/transform"
	ds "github.com/Groupe-Hevea/neosync/worker/pkg/benthos/transformers/data-sets"
)

// deterministicValueTransformer renvoie un transformer déterministe pour les
// configs reconnues, sinon (nil, false) — l'appelant retombe alors sur
// l'adaptateur Benthos aléatoire. Un deriver nil désactive tout (comportement
// historique).
//
// On traite ensemble les variantes Generate* et Transform* : sous Athanor, toutes
// deviennent une fonction déterministe de la valeur d'entrée (RFC §8). Les
// dictionnaires (prénom, nom, ville) passent par DictFaker ; les formats
// quasi-uniques (email, téléphone) par des fakers dédiés dérivés de la graine.
func deterministicValueTransformer(
	d *consistency.Deriver,
	cfg *mgmtv1alpha1.TransformerConfig,
) (transform.ValueTransformer, bool) {
	if d == nil || cfg == nil {
		return nil, false
	}

	switch {
	case cfg.GetGenerateFirstNameConfig() != nil || cfg.GetTransformFirstNameConfig() != nil:
		return native.NewDictFaker(d.Domain("person.first_name"), ds.FirstNames), true
	case cfg.GetGenerateLastNameConfig() != nil || cfg.GetTransformLastNameConfig() != nil:
		return native.NewDictFaker(d.Domain("person.last_name"), ds.LastNames), true
	case cfg.GetGenerateCityConfig() != nil:
		return native.NewDictFaker(d.Domain("geo.city"), ds.Address_Citys), true
	case cfg.GetGenerateEmailConfig() != nil || cfg.GetTransformEmailConfig() != nil:
		return native.NewEmailFaker(d.Domain("person.email"), ds.EmailDomains), true
	case cfg.GetTransformPhoneNumberConfig() != nil ||
		cfg.GetTransformE164PhoneNumberConfig() != nil ||
		cfg.GetGenerateE164PhoneNumberConfig() != nil:
		return native.NewPhoneFaker(d.Domain("person.phone")), true
	default:
		return nil, false
	}
}
