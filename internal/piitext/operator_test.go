package piitext

import (
	"context"
	"testing"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/stretchr/testify/require"
)

// constant is a builder of snippet transformers that all answer out, and counts its builds.
type constant struct {
	out    string
	builds int
}

func (c *constant) build(context.Context, *mgmtv1alpha1.TransformerConfig) (SnippetTransformer, error) {
	c.builds++
	return func(context.Context, string) (string, error) { return c.out, nil }, nil
}

func Test_Operator_Selection(t *testing.T) {
	text := "Call Jörg Müller in Paris"
	person := found(t, text, "Jörg Müller", "PERSON", 0.85)
	place := found(t, text, "Paris", "LOCATION", 0.85)

	withEntity := func(fallback, forPerson *mgmtv1alpha1.PiiAnonymizer) *mgmtv1alpha1.TransformPiiText {
		return &mgmtv1alpha1.TransformPiiText{
			DefaultAnonymizer: fallback,
			EntityAnonymizers: map[string]*mgmtv1alpha1.PiiAnonymizer{"PERSON": forPerson},
		}
	}

	t.Run("an entity set to replace is replaced, and the others get the default", func(t *testing.T) {
		out := mustRewrite(t, withEntity(redact(), replaceWith("X")), Options{}, text, person, place)
		require.Equal(t, "Call X in ", out)
	})

	t.Run("an entity set to redact is removed, and the others get the default", func(t *testing.T) {
		out := mustRewrite(t, withEntity(replaceWith("X"), redact()), Options{}, text, person, place)
		require.Equal(t, "Call  in X", out)
	})

	t.Run("an entity set to mask is masked, and the others get the default", func(t *testing.T) {
		out := mustRewrite(t, withEntity(redact(), mask("*", 4, false)), Options{}, text, person, place)
		require.Equal(t, "Call **** Müller in ", out)
	})

	t.Run("an entity set to hash is hashed, and the others get the default", func(t *testing.T) {
		config := withEntity(redact(), hashOf(mgmtv1alpha1.PiiAnonymizer_Hash_HASH_TYPE_SHA256))
		out := mustRewrite(t, config, Options{}, text, person, place)
		require.Regexp(t, `^Call [0-9a-f]{64} in $`, out)
	})

	t.Run("an entity set to transform is transformed, and the others get the default", func(t *testing.T) {
		builder := &bracketing{}
		out := mustRewrite(t, withEntity(redact(), transformWith(passthrough)), Options{Build: builder.build}, text, person, place)
		require.Equal(t, "Call [Jörg Müller] in ", out)
	})

	t.Run("an entity whose entry sets no kind gets the default", func(t *testing.T) {
		out := mustRewrite(t, withEntity(replaceWith("X"), &mgmtv1alpha1.PiiAnonymizer{}), Options{}, text, person)
		require.Equal(t, "Call X in Paris", out)
	})

	t.Run("an entity whose entry sets no kind, without a default, is written as its type", func(t *testing.T) {
		out := mustRewrite(t, withEntity(nil, &mgmtv1alpha1.PiiAnonymizer{}), Options{}, text, person)
		require.Equal(t, "Call <PERSON> in Paris", out)
	})

	t.Run("a default that sets no kind writes the entity type", func(t *testing.T) {
		out := mustRewrite(t, withDefault(&mgmtv1alpha1.PiiAnonymizer{}), Options{}, text, place)
		require.Equal(t, "Call Jörg Müller in <LOCATION>", out)
	})

	t.Run("a replace with an empty value writes the entity type", func(t *testing.T) {
		out := mustRewrite(t, withDefault(replaceWith("")), Options{}, text, place)
		require.Equal(t, "Call Jörg Müller in <LOCATION>", out)
		out = mustRewrite(t, withDefault(replaceByEntity()), Options{}, text, place)
		require.Equal(t, "Call Jörg Müller in <LOCATION>", out)
	})

	t.Run("an entity of a deny list is written under its own name", func(t *testing.T) {
		out := mustRewrite(t, nil, Options{}, text, found(t, text, "Paris", "vip-list", 1))
		require.Equal(t, "Call Jörg Müller in <vip-list>", out)
	})

	t.Run("an operator of an entity that is not found changes nothing", func(t *testing.T) {
		out := mustRewrite(t, withEntity(replaceWith("X"), redact()), Options{}, text, place)
		require.Equal(t, "Call Jörg Müller in X", out)
	})
}

func Test_Operator_Mask(t *testing.T) {
	for _, tc := range []struct {
		name     string
		text     string
		found    string
		operator *mgmtv1alpha1.PiiAnonymizer
		want     string
	}{
		{"from the start", "Call Jörg Müller now", "Jörg Müller", mask("*", 4, false), "Call **** Müller now"},
		{"from the end", "Call Jörg Müller now", "Jörg Müller", mask("*", 4, true), "Call Jörg Mü**** now"},
		{"more characters than the finding has", "Call Jörg Müller now", "Jörg Müller", mask("*", 40, false), "Call *********** now"},
		{"more characters than the finding has, from the end", "Call Jörg Müller now", "Jörg Müller", mask("*", 40, true), "Call *********** now"},
		{"a wide masking character", "山田太郎さんに電話", "山田太郎", mask("＊", 2, false), "＊＊太郎さんに電話"},
		{"a count of zero masks nothing", "Call Bob now", "Bob", mask("*", 0, false), "Call Bob now"},
		{"a negative count masks nothing", "Call Bob now", "Bob", mask("*", -3, false), "Call Bob now"},
		{"an empty masking character removes what it masks", "Call Jörg Müller now", "Jörg Müller", mask("", 4, false), "Call  Müller now"},
		{"no count masks the whole finding", "Call Jörg Müller now", "Jörg Müller", maskAll("#"), "Call ########### now"},
		{"no count and no character removes the finding", "Call Bob now", "Bob", maskAll(""), "Call  now"},
		{"characters are counted, not bytes", "Call éé now", "éé", mask("*", 1, true), "Call é* now"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := mustRewrite(t, withDefault(tc.operator), Options{}, tc.text, found(t, tc.text, tc.found, "PERSON", 0.85))
			require.Equal(t, tc.want, out)
		})
	}
}

func Test_Transform_Snippets(t *testing.T) {
	t.Run("a generator receives exactly the accented name it replaces", func(t *testing.T) {
		text := "Zoé a écrit"
		builder := &bracketing{}
		config := &mgmtv1alpha1.TransformPiiText{
			EntityAnonymizers: map[string]*mgmtv1alpha1.PiiAnonymizer{"PERSON": transformWith(passthrough)},
		}
		out := mustRewrite(t, config, Options{Build: builder.build}, text, found(t, text, "Zoé", "PERSON", 0.85))
		require.Equal(t, []string{"Zoé"}, builder.snippets)
		require.Equal(t, "[Zoé] a écrit", out)
	})

	t.Run("a transformer receives the whole address that follows accented letters", func(t *testing.T) {
		text := "Zoé a écrit à bob@example.com hier"
		builder := &bracketing{}
		out := mustRewrite(t, withDefault(transformWith(passthrough)), Options{Build: builder.build}, text,
			found(t, text, "bob@example.com", "EMAIL_ADDRESS", 1))
		require.Equal(t, []string{"bob@example.com"}, builder.snippets)
		require.Equal(t, "Zoé a écrit à [bob@example.com] hier", out)
	})

	t.Run("two findings of one entity type each get the output computed from their own text", func(t *testing.T) {
		text := "Bob met Zoé, then Bob left"
		builder := &bracketing{}
		config := &mgmtv1alpha1.TransformPiiText{
			EntityAnonymizers: map[string]*mgmtv1alpha1.PiiAnonymizer{"PERSON": transformWith(passthrough)},
		}
		// The analyzer answers in the order of its scores, not of the text.
		out := mustRewrite(t, config, Options{Build: builder.build}, text,
			found(t, text, "Zoé", "PERSON", 0.9), found(t, text, "Bob", "PERSON", 0.5))
		require.Equal(t, "[Bob] met [Zoé], then Bob left", out)
	})

	t.Run("findings of two entity types under a transform default each land on their own finding", func(t *testing.T) {
		text := "Bob in Paris"
		builder := &bracketing{}
		out := mustRewrite(t, withDefault(transformWith(passthrough)), Options{Build: builder.build}, text,
			found(t, text, "Paris", "LOCATION", 0.9), found(t, text, "Bob", "PERSON", 0.5))
		require.Equal(t, "[Bob] in [Paris]", out)
	})

	t.Run("overlapping findings of two entity types each land on the characters they own", func(t *testing.T) {
		text := "James Bond met Zoé"
		builder := &bracketing{}
		out := mustRewrite(t, withDefault(transformWith(passthrough)), Options{Build: builder.build}, text,
			found(t, text, "James Bo", "PERSON", 0.9), found(t, text, "Bond", "LOCATION", 0.4))
		require.Equal(t, "[James Bo][nd] met Zoé", out)
	})

	t.Run("a text in double braces is left where it is", func(t *testing.T) {
		text := "{{HUSONYM_PERSON}} met Bob, see {{HUSONYM_DEFAULT}}"
		out := mustRewrite(t, withDefault(transformWith(nil)), Options{Build: (&constant{out: "Ann"}).build}, text,
			found(t, text, "Bob", "PERSON", 0.85))
		require.Equal(t, "{{HUSONYM_PERSON}} met Ann, see {{HUSONYM_DEFAULT}}", out)
	})

	t.Run("an output that equals another finding is not rewritten again", func(t *testing.T) {
		text := "Bob met Zoé"
		out := mustRewrite(t, withDefault(transformWith(nil)), Options{Build: (&constant{out: "Zoé"}).build}, text,
			found(t, text, "Bob", "PERSON", 0.85), found(t, text, "Zoé", "PERSON", 0.85))
		require.Equal(t, "Zoé met Zoé", out)
	})

	t.Run("an empty output removes the finding", func(t *testing.T) {
		text := "Bob met Zoé"
		out := mustRewrite(t, withDefault(transformWith(nil)), Options{Build: (&constant{}).build}, text,
			found(t, text, "Zoé", "PERSON", 0.85))
		require.Equal(t, "Bob met ", out)
	})

	t.Run("an output longer than the finding moves nothing else", func(t *testing.T) {
		text := "Bob met Zoé and Al"
		out := mustRewrite(t, withDefault(transformWith(nil)), Options{Build: (&constant{out: "Maximilian"}).build}, text,
			found(t, text, "Bob", "PERSON", 0.85), found(t, text, "Al", "PERSON", 0.85))
		require.Equal(t, "Maximilian met Zoé and Maximilian", out)
	})

	t.Run("the transformer of an operator is built once, whatever the number of findings and values", func(t *testing.T) {
		text := "Bob met Zoé"
		builder := &constant{out: "N"}
		transformer, err := newEngine(t, finding(t,
			found(t, text, "Bob", "PERSON", 0.85), found(t, text, "Zoé", "PERSON", 0.85),
		)).Transformer(withDefault(transformWith(passthrough)), Options{Build: builder.build})
		require.NoError(t, err)
		for range 3 {
			out, err := transformer.Transform(context.Background(), text)
			require.NoError(t, err)
			require.Equal(t, "N met N", out)
		}
		require.Equal(t, 1, builder.builds)
	})

	t.Run("a transformer that is not used is not built", func(t *testing.T) {
		text := "Bob met Zoé"
		builder := &constant{out: "N"}
		config := &mgmtv1alpha1.TransformPiiText{
			EntityAnonymizers: map[string]*mgmtv1alpha1.PiiAnonymizer{"LOCATION": transformWith(passthrough)},
		}
		out := mustRewrite(t, config, Options{Build: builder.build}, text, found(t, text, "Bob", "PERSON", 0.85))
		require.Equal(t, "<PERSON> met Zoé", out)
		require.Zero(t, builder.builds)
	})
}

func Test_Transform_ChosenByEntity(t *testing.T) {
	chosen := func(t *testing.T, config *mgmtv1alpha1.TransformPiiText, entity string) *mgmtv1alpha1.TransformerConfig {
		t.Helper()
		text := "x Bob y"
		builder := &bracketing{}
		mustRewrite(t, config, Options{Build: builder.build}, text, found(t, text, "Bob", entity, 0.85))
		require.Len(t, builder.built, 1)
		return builder.built[0]
	}
	kinds := map[string]func(*mgmtv1alpha1.TransformerConfig) bool{
		"CREDIT_CARD": func(c *mgmtv1alpha1.TransformerConfig) bool {
			return c.GetGenerateCardNumberConfig().GetValidLuhn()
		},
		"PERSON":       func(c *mgmtv1alpha1.TransformerConfig) bool { return c.GetGenerateFullNameConfig() != nil },
		"PHONE_NUMBER": func(c *mgmtv1alpha1.TransformerConfig) bool { return c.GetGenerateStringPhoneNumberConfig() != nil },
		"US_SSN":       func(c *mgmtv1alpha1.TransformerConfig) bool { return c.GetGenerateSsnConfig() != nil },
		"IP_ADDRESS": func(c *mgmtv1alpha1.TransformerConfig) bool {
			return c.GetGenerateIpAddressConfig().GetIpType() == mgmtv1alpha1.GenerateIpAddressType_GENERATE_IP_ADDRESS_TYPE_V4_PUBLIC
		},
		"EMAIL_ADDRESS": func(c *mgmtv1alpha1.TransformerConfig) bool {
			email := c.GetTransformEmailConfig()
			return email.GetEmailType() == mgmtv1alpha1.GenerateEmailType_GENERATE_EMAIL_TYPE_UUID_V4 &&
				email.GetInvalidEmailAction() == mgmtv1alpha1.InvalidEmailAction_INVALID_EMAIL_ACTION_GENERATE
		},
		"LOCATION": func(c *mgmtv1alpha1.TransformerConfig) bool { return c.GetGenerateSha256HashConfig() != nil },
		"vip-list": func(c *mgmtv1alpha1.TransformerConfig) bool { return c.GetGenerateSha256HashConfig() != nil },
	}
	for entity, isExpected := range kinds {
		t.Run("a default transform without a transformer picks one for "+entity, func(t *testing.T) {
			require.True(t, isExpected(chosen(t, withDefault(transformWith(nil)), entity)))
		})
		t.Run("an entity transform without a transformer picks one for "+entity, func(t *testing.T) {
			config := &mgmtv1alpha1.TransformPiiText{
				DefaultAnonymizer: redact(),
				EntityAnonymizers: map[string]*mgmtv1alpha1.PiiAnonymizer{entity: transformWith(nil)},
			}
			require.True(t, isExpected(chosen(t, config, entity)))
		})
	}

	t.Run("a transform with a transformer uses it, whatever the entity", func(t *testing.T) {
		require.NotNil(t, chosen(t, withDefault(transformWith(passthrough)), "PERSON").GetPassthroughConfig())
	})
}

func Test_Validate(t *testing.T) {
	nested := &mgmtv1alpha1.TransformerConfig{Config: &mgmtv1alpha1.TransformerConfig_TransformPiiTextConfig{
		TransformPiiTextConfig: &mgmtv1alpha1.TransformPiiText{},
	}}

	t.Run("no configuration and an empty one can be applied", func(t *testing.T) {
		require.NoError(t, Validate(nil))
		require.NoError(t, Validate(&mgmtv1alpha1.TransformPiiText{}))
	})

	t.Run("a default that transforms with this transformer is refused", func(t *testing.T) {
		err := Validate(withDefault(transformWith(nested)))
		var configErr *ConfigError
		require.ErrorAs(t, err, &configErr)
		require.Equal(t,
			"found nested TransformPiiText config in default anonymizer. TransformPiiText may not be used deeply nested within itself.",
			err.Error())
	})

	t.Run("an entity that transforms with this transformer is refused", func(t *testing.T) {
		err := Validate(&mgmtv1alpha1.TransformPiiText{
			EntityAnonymizers: map[string]*mgmtv1alpha1.PiiAnonymizer{"PERSON": transformWith(nested)},
		})
		var configErr *ConfigError
		require.ErrorAs(t, err, &configErr)
		require.Equal(t,
			"found nested TransformPiiText config in entity (PERSON) anonymizer. TransformPiiText may not be used deeply nested within itself.",
			err.Error())
	})

	t.Run("a masking character of two characters is refused", func(t *testing.T) {
		var configErr *ConfigError
		require.ErrorAs(t, Validate(withDefault(mask("ab", 4, false))), &configErr)
		require.NoError(t, Validate(withDefault(mask("＊", 4, false))), "one character of several bytes is one character")
	})

	t.Run("a configuration that cannot be applied gives no transformer", func(t *testing.T) {
		transformer, err := newEngine(t, finding(t)).Transformer(withDefault(mask("ab", 4, false)), Options{})
		var configErr *ConfigError
		require.ErrorAs(t, err, &configErr)
		require.Nil(t, transformer)
	})
}
