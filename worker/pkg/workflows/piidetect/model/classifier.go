package model

import (
	"context"
	"errors"
	"time"

	"github.com/fishtre-compagnie/husonym/worker/pkg/workflows/piidetect/profile"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/shared"
)

const (
	// How many columns a request asks about. A profile has a bounded size, so the size
	// of a request follows from the number of its columns, whatever tokenizer the model
	// has. A column weighs more with its values.
	batchSize           = 25
	batchSizeWithValues = 15

	// requestTimeout bounds one request. A local model on a processor may take tens of
	// seconds for a batch.
	requestTimeout = 2 * time.Minute
)

// A request that carries values is tried this many times, after these waits, when its
// failure may heal.
var triesWithValues = []time.Duration{5 * time.Second, 10 * time.Second}

// Column is what the model is told of a column.
type Column struct {
	Name, DataType string
	Nullable       bool
	Profile        *profile.Profile
	// Values are sample values of the column, when the job sends some.
	Values []string
}

// Table is what the model is told of the table of the columns.
type Table struct {
	Schema, Name string
	// Hints are the notes of the job's owner about the schema.
	Hints string
}

// Classifier asks the configured model.
type Classifier struct {
	cfg            Config
	chat           openai.ChatCompletionService
	requestTimeout time.Duration
	wait           func(context.Context, time.Duration) error
}

// NewClassifier returns the classifier of a configuration, nil when it names no model.
//
// The client does not retry by itself: the caller's retries are the only ones, so that a
// failed request is counted once.
func NewClassifier(cfg Config) (*Classifier, error) {
	if !cfg.Enabled() {
		return nil, nil
	}
	options := []option.RequestOption{option.WithMaxRetries(0)}
	if cfg.BaseURL != "" {
		options = append(options, option.WithBaseURL(cfg.BaseURL))
	}
	if cfg.APIKey != "" {
		options = append(options, option.WithAPIKey(cfg.APIKey))
	}
	client := openai.NewClient(options...)
	return &Classifier{
		cfg:            cfg,
		chat:           client.Chat.Completions,
		requestTimeout: requestTimeout,
		wait:           wait,
	}, nil
}

// Model returns the name of the model that is asked.
func (c *Classifier) Model() string {
	return c.cfg.Model
}

func wait(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// Batches cuts the columns of a table into the requests they are asked in, in their
// order.
func Batches(columns []Column) [][]Column {
	size := batchSize
	if carryValues(columns) {
		size = batchSizeWithValues
	}
	var batches [][]Column
	for len(columns) > size {
		batches = append(batches, columns[:size])
		columns = columns[size:]
	}
	if len(columns) > 0 {
		batches = append(batches, columns)
	}
	return batches
}

func carryValues(columns []Column) bool {
	for _, column := range columns {
		if len(column.Values) > 0 {
			return true
		}
	}
	return false
}

// Classify asks the model about one batch. A column without a valid answer is asked once
// more, with the others in its case, in one request; what is still missing is returned as
// unanswered. The error is an *Error, or the error of a context that ended.
//
// A request that carries values is repeated in place when its failure may heal. The
// values of a table are picked once: the caller cannot run again without picking others,
// so it leaves the repeats to this call, which sends the very same request.
func (c *Classifier) Classify(ctx context.Context, t Table, batch []Column) (*Result, error) {
	result := &Result{}
	missing, err := c.ask(ctx, t, batch, result)
	if err != nil {
		return nil, err
	}
	if len(missing) == 0 {
		return result, nil
	}
	stillMissing, err := c.ask(ctx, t, missing, result)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		// The first answer stands: what it lacks is reported, not asked again.
		stillMissing = missing
	}
	for _, column := range stillMissing {
		result.Unanswered = append(result.Unanswered, column.Name)
	}
	return result, nil
}

// ask sends one request about the columns, files their valid answers in result, and
// returns the columns that have none.
func (c *Classifier) ask(ctx context.Context, t Table, columns []Column, result *Result) ([]Column, error) {
	withValues := carryValues(columns)
	content, err := userMessage(t, columns)
	if err != nil {
		return nil, &Error{Reason: ReasonRejected, Detail: "the request could not be written"}
	}
	params := openai.ChatCompletionNewParams{
		Model:       c.cfg.Model,
		Temperature: openai.Float(0),
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage(systemMessage(withValues)),
			openai.UserMessage(content),
		},
		ResponseFormat: openai.ChatCompletionNewParamsResponseFormatUnion{
			OfJSONSchema: &shared.ResponseFormatJSONSchemaParam{
				JSONSchema: shared.ResponseFormatJSONSchemaJSONSchemaParam{
					Name:   "column_categories",
					Strict: openai.Bool(true),
					Schema: answerSchema(len(columns)),
				},
			},
		},
	}

	completion, err := c.send(ctx, &params, withValues)
	if err != nil {
		return nil, err
	}
	answers, ignored := readAnswer(completion, len(columns))
	result.Ignored += ignored

	var missing []Column
	for i, column := range columns {
		answer, ok := answers[columnId(i)]
		if !ok {
			missing = append(missing, column)
			continue
		}
		result.keep(column.Name, answer, c.cfg.MinConfidence)
	}
	return missing, nil
}

func (c *Classifier) send(
	ctx context.Context,
	params *openai.ChatCompletionNewParams,
	withValues bool,
) (*openai.ChatCompletion, error) {
	var waits []time.Duration
	if withValues {
		waits = triesWithValues
	}
	for try := 0; ; try++ {
		completion, err := c.chat.New(ctx, *params, option.WithRequestTimeout(c.requestTimeout))
		if err == nil {
			return completion, nil
		}
		failed := failure(ctx, err, withValues)
		var modelErr *Error
		if !errors.As(failed, &modelErr) || modelErr.Permanent() || try >= len(waits) {
			return nil, failed
		}
		if err := c.wait(ctx, waits[try]); err != nil {
			return nil, err
		}
	}
}
