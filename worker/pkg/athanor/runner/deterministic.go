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
	"fmt"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	pseudo_functions "github.com/fishtre-compagnie/husonym/internal/javascript/functions/pseudo"
	"github.com/fishtre-compagnie/husonym/worker/pkg/athanor/consistency"
	"github.com/fishtre-compagnie/husonym/worker/pkg/athanor/native"
	"github.com/fishtre-compagnie/husonym/worker/pkg/athanor/transform"
	ds "github.com/fishtre-compagnie/husonym/worker/pkg/benthos/transformers/data-sets"
)

// deterministicValueTransformer renvoie un transformer déterministe pour les
// configs reconnues, sinon (nil, false) — l'appelant retombe alors sur
// l'adaptateur Benthos aléatoire. Un deriver nil désactive tout (comportement
// historique).
//
// Generate* and Transform* variants are handled together: under Athanor both
// become a deterministic function of the input value (RFC §8). Dictionary-backed
// kinds (first name, last name, city) go through DictFaker; near-unique formats
// (email, phone) through dedicated fakers derived from the seed.
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
	case cfg.GetGenerateFullNameConfig() != nil || cfg.GetTransformFullNameConfig() != nil:
		return native.NewFullNameFaker(d.Domain("person.full_name"), ds.FirstNames, ds.LastNames), true
	case cfg.GetGenerateCityConfig() != nil:
		return native.NewDictFaker(d.Domain("geo.city"), ds.Address_Citys), true
	case cfg.GetGenerateStateConfig() != nil:
		return native.NewDictFaker(d.Domain("geo.state"), ds.UsStates), true
	case cfg.GetGenerateZipcodeConfig() != nil:
		return native.NewDictFaker(d.Domain("geo.postal_code"), ds.Address_ZipCodes), true
	case cfg.GetGenerateStreetAddressConfig() != nil:
		return native.NewDictFaker(d.Domain("geo.street_address"), ds.Address_Address1s), true
	case cfg.GetGenerateCountryConfig() != nil:
		return native.NewDictFaker(d.Domain("geo.country"), ds.Countrys), true
	case cfg.GetGenerateBusinessNameConfig() != nil:
		return native.NewDictFaker(d.Domain("company.name"), ds.BusinessNames), true
	case cfg.GetGenerateEmailConfig() != nil || cfg.GetTransformEmailConfig() != nil:
		// Casse conservée : une colonne email unique et sensible à la casse peut
		// contenir "Bob@x.com" et "bob@x.com" ; les fusionner casserait l'unicité.
		return native.NewEmailFaker(d.Domain("person.email").WithCanonicalizer(consistency.PreserveCase), ds.EmailDomains), true
	case cfg.GetTransformPhoneNumberConfig().GetPreserveFormat():
		return native.NewPhoneFormatPreserver(d.CipherKey("person.phone")), true
	case cfg.GetTransformPhoneNumberConfig() != nil ||
		cfg.GetTransformE164PhoneNumberConfig() != nil ||
		cfg.GetGenerateE164PhoneNumberConfig() != nil:
		return native.NewPhoneFaker(d.Domain("person.phone")), true
	default:
		return nil, false
	}
}

// pseudoConfigs holds, for each function pseudo.<kind> offered to scripts, the transformer
// whose output it returns.
var pseudoConfigs = map[string]*mgmtv1alpha1.TransformerConfig{
	"firstName": {Config: &mgmtv1alpha1.TransformerConfig_GenerateFirstNameConfig{
		GenerateFirstNameConfig: &mgmtv1alpha1.GenerateFirstName{},
	}},
	"lastName": {Config: &mgmtv1alpha1.TransformerConfig_GenerateLastNameConfig{
		GenerateLastNameConfig: &mgmtv1alpha1.GenerateLastName{},
	}},
	"fullName": {Config: &mgmtv1alpha1.TransformerConfig_GenerateFullNameConfig{
		GenerateFullNameConfig: &mgmtv1alpha1.GenerateFullName{},
	}},
	"email": {Config: &mgmtv1alpha1.TransformerConfig_GenerateEmailConfig{
		GenerateEmailConfig: &mgmtv1alpha1.GenerateEmail{},
	}},
	"phone": {Config: &mgmtv1alpha1.TransformerConfig_GenerateE164PhoneNumberConfig{
		GenerateE164PhoneNumberConfig: &mgmtv1alpha1.GenerateE164PhoneNumber{},
	}},
	"city": {Config: &mgmtv1alpha1.TransformerConfig_GenerateCityConfig{
		GenerateCityConfig: &mgmtv1alpha1.GenerateCity{},
	}},
	"state": {Config: &mgmtv1alpha1.TransformerConfig_GenerateStateConfig{
		GenerateStateConfig: &mgmtv1alpha1.GenerateState{},
	}},
	"zipcode": {Config: &mgmtv1alpha1.TransformerConfig_GenerateZipcodeConfig{
		GenerateZipcodeConfig: &mgmtv1alpha1.GenerateZipcode{},
	}},
	"streetAddress": {Config: &mgmtv1alpha1.TransformerConfig_GenerateStreetAddressConfig{
		GenerateStreetAddressConfig: &mgmtv1alpha1.GenerateStreetAddress{},
	}},
	"country": {Config: &mgmtv1alpha1.TransformerConfig_GenerateCountryConfig{
		GenerateCountryConfig: &mgmtv1alpha1.GenerateCountry{},
	}},
	"businessName": {Config: &mgmtv1alpha1.TransformerConfig_GenerateBusinessNameConfig{
		GenerateBusinessNameConfig: &mgmtv1alpha1.GenerateBusinessName{},
	}},
}

// pseudoSource is the consistency scope of a run, as the scripts reach it through the
// pseudo functions: the same fakes as the native transformers, and seeds in the domains a
// rule names, apart from theirs.
type pseudoSource struct {
	deriver *consistency.Deriver
	fakes   map[string]transform.ValueTransformer
}

var _ pseudo_functions.Source = (*pseudoSource)(nil)

// newPseudoSource returns nil without a deriver: the pseudo functions then fail.
func newPseudoSource(d *consistency.Deriver) *pseudoSource {
	if d == nil {
		return nil
	}
	fakes := make(map[string]transform.ValueTransformer, len(pseudoConfigs))
	for kind, cfg := range pseudoConfigs {
		if vt, ok := deterministicValueTransformer(d, cfg); ok {
			fakes[kind] = vt
		}
	}
	return &pseudoSource{deriver: d, fakes: fakes}
}

func (s *pseudoSource) Fake(kind string, value any) (any, error) {
	fake, ok := s.fakes[kind]
	if !ok {
		return nil, fmt.Errorf("no deterministic transformer for %q", kind)
	}
	return fake.TransformValue(transform.Background(), value)
}

func (s *pseudoSource) Seed(domain string, value any) consistency.Seed {
	return s.deriver.Domain("user:" + domain).WithCanonicalizer(consistency.Exact).Seed(fmt.Sprint(value))
}
