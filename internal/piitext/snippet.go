package piitext

import (
	"context"
	"errors"
	"sync"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"google.golang.org/protobuf/proto"
)

// SnippetTransformer rewrites one snippet of personal data.
type SnippetTransformer func(ctx context.Context, snippet string) (string, error)

// SnippetTransformerBuilder builds the SnippetTransformer of a Husonym transformer
// configuration. It is called at most once per operator of a Transformer.
type SnippetTransformerBuilder func(
	ctx context.Context,
	config *mgmtv1alpha1.TransformerConfig,
) (SnippetTransformer, error)

// snippets are the snippet transformers of the transform operators of one Transformer, each
// built when a finding first needs it and kept for the next ones.
type snippets struct {
	build SnippetTransformerBuilder

	mu    sync.Mutex
	built map[string]SnippetTransformer
}

// operator returns the transform operator of an anonymizer. With a transformer configured, it
// runs it on every finding. Without one, it runs the transformer chosen for the entity type of
// the finding.
func (s *snippets) operator(name string, config *mgmtv1alpha1.TransformerConfig) operator {
	return func(ctx context.Context, entity, text string) (string, error) {
		name, config := name, config
		if config == nil {
			var kind string
			kind, config = chosenFor(entity)
			name = "chosen:" + kind
		}
		transformer, err := s.get(ctx, name, config)
		if err != nil {
			return "", err
		}
		return transformer(ctx, text)
	}
}

func (s *snippets) get(
	ctx context.Context,
	name string,
	config *mgmtv1alpha1.TransformerConfig,
) (SnippetTransformer, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if transformer, ok := s.built[name]; ok {
		return transformer, nil
	}
	if s.build == nil {
		return nil, errors.New("no Husonym transformer can be run here")
	}
	transformer, err := s.build(ctx, config)
	if err != nil {
		return nil, err
	}
	if s.built == nil {
		s.built = map[string]SnippetTransformer{}
	}
	s.built[name] = transformer
	return transformer, nil
}

// chosenFor returns the transformer of a transform operator that names none, for an entity
// type, and what tells it from the others.
func chosenFor(entity string) (kind string, config *mgmtv1alpha1.TransformerConfig) {
	switch entity {
	case "CREDIT_CARD":
		return "card", &mgmtv1alpha1.TransformerConfig{Config: &mgmtv1alpha1.TransformerConfig_GenerateCardNumberConfig{
			GenerateCardNumberConfig: &mgmtv1alpha1.GenerateCardNumber{ValidLuhn: proto.Bool(true)},
		}}
	case "PERSON":
		return "name", &mgmtv1alpha1.TransformerConfig{Config: &mgmtv1alpha1.TransformerConfig_GenerateFullNameConfig{
			GenerateFullNameConfig: &mgmtv1alpha1.GenerateFullName{},
		}}
	case "PHONE_NUMBER":
		return "phone", &mgmtv1alpha1.TransformerConfig{Config: &mgmtv1alpha1.TransformerConfig_GenerateStringPhoneNumberConfig{
			GenerateStringPhoneNumberConfig: &mgmtv1alpha1.GenerateStringPhoneNumber{},
		}}
	case "US_SSN":
		return "ssn", &mgmtv1alpha1.TransformerConfig{Config: &mgmtv1alpha1.TransformerConfig_GenerateSsnConfig{
			GenerateSsnConfig: &mgmtv1alpha1.GenerateSSN{},
		}}
	case "IP_ADDRESS":
		return "ip", &mgmtv1alpha1.TransformerConfig{Config: &mgmtv1alpha1.TransformerConfig_GenerateIpAddressConfig{
			GenerateIpAddressConfig: &mgmtv1alpha1.GenerateIpAddress{
				IpType: mgmtv1alpha1.GenerateIpAddressType_GENERATE_IP_ADDRESS_TYPE_V4_PUBLIC.Enum(),
			},
		}}
	case "EMAIL_ADDRESS":
		return "email", &mgmtv1alpha1.TransformerConfig{Config: &mgmtv1alpha1.TransformerConfig_TransformEmailConfig{
			TransformEmailConfig: &mgmtv1alpha1.TransformEmail{
				EmailType:          mgmtv1alpha1.GenerateEmailType_GENERATE_EMAIL_TYPE_UUID_V4.Enum(),
				InvalidEmailAction: mgmtv1alpha1.InvalidEmailAction_INVALID_EMAIL_ACTION_GENERATE.Enum(),
			},
		}}
	default:
		return "hash", &mgmtv1alpha1.TransformerConfig{Config: &mgmtv1alpha1.TransformerConfig_GenerateSha256HashConfig{
			GenerateSha256HashConfig: &mgmtv1alpha1.GenerateSha256Hash{},
		}}
	}
}
