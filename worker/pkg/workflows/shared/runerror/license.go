package runerror

import "errors"

// licensed marks an error as a refusal of the license. Its text is that of the error it
// holds, which it gives without reading it: the method is the one of the embedded error.
type licensed struct{ error }

func (l *licensed) Unwrap() error { return l.error }

// License marks err as a refusal of the license: Classify and CategoryOf read it as such,
// wherever it is in an error. The text of the error is unchanged and err is still found
// under it. The mark is a Go type: it does not cross the boundary of an activity or of a
// child workflow by itself, the category Carry gives the error does.
func License(err error) error {
	if err == nil {
		return nil
	}
	return &licensed{error: err}
}

func isLicense(err error) bool {
	var mark *licensed
	return errors.As(err, &mark)
}
