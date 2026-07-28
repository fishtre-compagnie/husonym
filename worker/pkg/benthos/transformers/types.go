package transformers

type TemplateData struct {
	Name        string
	Description string
	Example     string
}

type HusonymTransformer interface {
	ParseOptions(opts map[string]any) (any, error)
	GetJsTemplateData() (*TemplateData, error)
	Transform(value any, opts any) (any, error)
}

type HusonymGenerator interface {
	ParseOptions(opts map[string]any) (any, error)
	GetJsTemplateData() (*TemplateData, error)
	Generate(opts any) (any, error)
}
