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

// dictBinding = un type sémantique (clé de domaine de cohérence) et son
// dictionnaire faker versionné.
type dictBinding struct {
	semanticType string
	dict         []string
}

// deterministicValueTransformer renvoie un transformer déterministe (DictFaker)
// pour les configs reconnues, sinon (nil, false) — l'appelant retombe alors sur
// l'adaptateur Benthos aléatoire. Un deriver nil désactive tout (comportement
// historique).
//
// On traite ensemble les variantes Generate* et Transform* : sous Athanor, toutes
// deviennent une fonction déterministe de la valeur d'entrée (RFC §8).
func deterministicValueTransformer(
	d *consistency.Deriver,
	cfg *mgmtv1alpha1.TransformerConfig,
) (transform.ValueTransformer, bool) {
	if d == nil || cfg == nil {
		return nil, false
	}

	var b dictBinding
	switch {
	case cfg.GetGenerateFirstNameConfig() != nil || cfg.GetTransformFirstNameConfig() != nil:
		b = dictBinding{"person.first_name", ds.FirstNames}
	case cfg.GetGenerateLastNameConfig() != nil || cfg.GetTransformLastNameConfig() != nil:
		b = dictBinding{"person.last_name", ds.LastNames}
	case cfg.GetGenerateCityConfig() != nil:
		b = dictBinding{"geo.city", ds.Address_Citys}
	default:
		return nil, false
	}

	return native.NewDictFaker(d.Domain(b.semanticType), b.dict), true
}
