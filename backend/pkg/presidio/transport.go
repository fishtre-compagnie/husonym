package presidio

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

const (
	// maxAnswerBytes is the largest success read: the list of what was found in a text. Past
	// it the answer is not one to a text.
	maxAnswerBytes = 16 << 20
	// maxRefusalBytes is how much of a refusal is read: its message is a sentence.
	maxRefusalBytes = 64 << 10
)

// endpoint is a service of Presidio, and how it is reached.
type endpoint struct {
	base       *url.URL
	httpClient *http.Client
	timeout    time.Duration
}

func newEndpoint(baseURL string, httpClient *http.Client) (*endpoint, error) {
	if httpClient == nil {
		return nil, errors.New("presidio: no HTTP client")
	}
	base, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("presidio: invalid URL: %w", err)
	}
	if (base.Scheme != "http" && base.Scheme != "https") || base.Host == "" {
		return nil, fmt.Errorf("presidio: the URL %q is not an absolute http(s) URL", baseURL)
	}
	return &endpoint{base: base, httpClient: httpClient, timeout: Timeout}, nil
}

// do makes one call and returns the body of a success. The whole exchange, the reading of the
// answer included, is given the time of the endpoint, or what the caller has left when it is
// less.
func (e *endpoint) do(
	ctx context.Context,
	operation, method, path string,
	query url.Values,
	payload any,
) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, e.timeout)
	defer cancel()

	var body io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("presidio %s: unable to encode the request: %w", operation, err)
		}
		body = bytes.NewReader(encoded)
	}
	target := e.base.JoinPath(path)
	target.RawQuery = query.Encode()
	req, err := http.NewRequestWithContext(ctx, method, target.String(), body)
	if err != nil {
		return nil, fmt.Errorf("presidio %s: unable to build the request: %w", operation, err)
	}
	req.Header.Set("Accept", "application/json")
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := e.httpClient.Do(req)
	if err != nil {
		return nil, noAnswer(operation, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		// One byte past the limit tells an answer that fills it from one that exceeds it.
		answer, err := io.ReadAll(io.LimitReader(resp.Body, maxAnswerBytes+1))
		if err != nil {
			return nil, noAnswer(operation, err)
		}
		if len(answer) > maxAnswerBytes {
			return nil, invalid(operation, "the answer is larger than %d bytes", maxAnswerBytes)
		}
		return answer, nil
	}

	refusal, err := io.ReadAll(io.LimitReader(resp.Body, maxRefusalBytes))
	if err != nil {
		return nil, noAnswer(operation, err)
	}
	if notPresidio(resp.StatusCode) {
		return nil, noAnswer(operation, fmt.Errorf("status %d", resp.StatusCode))
	}
	return nil, &RefusedError{
		Operation:  operation,
		StatusCode: resp.StatusCode,
		Message:    refusalMessage(refusal),
	}
}

// notPresidio says whether a status is that of what stands before Presidio — a proxy that
// cannot reach it, or waited for it too long — or of a Presidio that takes no more.
func notPresidio(status int) bool {
	switch status {
	case http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	default:
		return false
	}
}

func noAnswer(operation string, cause error) error {
	return fmt.Errorf("presidio %s: %w", operation, errors.Join(ErrNoAnswer, cause))
}

// decode reads the body of a success as the JSON the operation answers with.
func decode[T any](operation string, answer []byte) (T, error) {
	var decoded T
	if err := json.Unmarshal(answer, &decoded); err != nil {
		return decoded, invalid(operation, "%v", err)
	}
	return decoded, nil
}
