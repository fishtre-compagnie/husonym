package native

import "github.com/fishtre-compagnie/husonym/worker/pkg/athanor/transform"

// Null writes a real SQL NULL whatever the input. The Benthos executor returns the
// string "null" for this transformer and relies on its SQL processor to turn it
// into NULL; Athanor binds values directly, so it must emit nil itself.
type Null struct{}

func (Null) TransformValue(transform.Ctx, any) (any, error) { return nil, nil }

var _ transform.ValueTransformer = Null{}
