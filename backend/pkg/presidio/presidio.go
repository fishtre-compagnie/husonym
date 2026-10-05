// Package presidio is the client of the analyzer of Microsoft Presidio, an open-source service
// (MIT) that finds personal data in a text. What is found is rewritten by the product itself
// (internal/piitext): Presidio's other service, the anonymizer, is not called.
//
// A call gives its result or one of three errors, told apart by what the caller can do:
// ErrNoAnswer, Presidio could not be asked; *RefusedError, it was asked and did not do it;
// ErrInvalidResponse, it answered something that cannot be read.
//
// https://microsoft.github.io/presidio/api-docs/api-docs.html
package presidio

import (
	"context"
	"time"
)

// Timeout is how long Presidio is waited for, for one call, from the connection to the last
// byte of the answer: a text may be a long one, on a Presidio that is busy. A caller that knows
// its texts are short sets a shorter limit on the context it calls with.
const Timeout = time.Minute

// Analyzer finds the personal data of one text.
type Analyzer interface {
	Analyze(ctx context.Context, req *AnalyzeRequest) ([]Finding, error)
}

// EntityLister tells which entity types the analyzer can find in a language.
type EntityLister interface {
	SupportedEntities(ctx context.Context, language string) ([]string, error)
}
