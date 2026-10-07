package usagereport

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/fishtre-compagnie/husonym/backend/internal/usagestore"
	"github.com/fishtre-compagnie/husonym/internal/safehttp"
	"github.com/fishtre-compagnie/husonym/internal/telemetry"
)

// DefaultReportURL is the address the usage report of the instance is sent to.
const DefaultReportURL = "https://license.husonym.com/v1/usage-reports"

const (
	// postTimeout bounds one request as a whole: connection, sending, and the reading of the
	// answer.
	postTimeout = 15 * time.Second
	// maxAnswerBytes is how much of an answer is read before it is dropped. Nothing is made of it.
	maxAnswerBytes = 64 << 10
)

// Transport posts one sealed report. It never follows a redirect.
type Transport interface {
	Post(ctx context.Context, report *usagestore.StoredReport) error
}

// HTTPTransport posts a report to one address, over HTTP.
type HTTPTransport struct {
	target *url.URL
	// host is how the address is named in an error: without its path nor its credentials.
	host      string
	userAgent string
	client    *http.Client
}

// NewHTTPTransport returns the transport to the given address, which is one a report can be
// sent to: an https URL with a host, or an http one, which only an operator who names its own
// receiver asks for. version is the version of this build.
func NewHTTPTransport(address, version string) (Transport, error) {
	target, err := checkReportURL(address)
	if err != nil {
		return nil, err
	}
	return newHTTPTransport(target, version, postTimeout), nil
}

// checkReportURL parses an address a report can be sent to. Its errors never quote the address,
// which may hold credentials.
func checkReportURL(address string) (*url.URL, error) {
	target, err := url.Parse(address)
	if err != nil {
		// The error of the parser quotes the address.
		return nil, errors.New("the address the report is sent to cannot be parsed")
	}
	if target.Hostname() == "" {
		return nil, errors.New("the address the report is sent to has no host")
	}
	if err := reportPolicy(target).CheckURL(target); err != nil {
		return nil, fmt.Errorf("the address the report is sent to cannot be used: %w", err)
	}
	return target, nil
}

// reportPolicy trusts the origin of the address the report is sent to, and nothing else: the
// address is given by this build or by the operator, never by an account, and a trusted origin
// is the one reached through the proxy of the environment. Nothing private is allowed beyond it.
func reportPolicy(target *url.URL) safehttp.Policy {
	return safehttp.NewPolicy(false, target.String())
}

func newHTTPTransport(target *url.URL, version string, timeout time.Duration) *HTTPTransport {
	return &HTTPTransport{
		target:    target,
		host:      target.Host,
		userAgent: "husonym/" + telemetry.HusonymVersion(version),
		client: &http.Client{
			// A report leaves once a day: each request has its own connection.
			Transport: reportPolicy(target).Transport(timeout, false),
			Timeout:   timeout,
			// A redirect is the answer, not a new destination: the document and its seal are
			// for the address the report is sent to.
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
	}
}

// Post sends the document of the report as it is stored, byte for byte, with its seal and the
// fingerprint of its key in headers. A nil error means the answer was a 2xx status. An error
// names the host and never quotes the address nor the answer.
func (t *HTTPTransport) Post(ctx context.Context, report *usagestore.StoredReport) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.target.String(), bytes.NewReader(report.Document))
	if err != nil {
		return fmt.Errorf("unable to build the request to %s", t.host)
	}
	// Asked here first: the error of the client would quote the proxy setting, which may hold
	// credentials.
	if _, err := http.ProxyFromEnvironment(req); err != nil {
		return errors.New("the proxy setting of the environment is not valid")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", t.userAgent)
	req.Header.Set("Husonym-Seal", report.Seal)
	req.Header.Set("Husonym-Key-Fingerprint", report.KeyFingerprint)

	resp, err := t.client.Do(req)
	if err != nil {
		// The error of the client quotes the address: only what it wraps is kept.
		var urlErr *url.Error
		if errors.As(err, &urlErr) {
			err = urlErr.Err
		}
		return fmt.Errorf("unable to reach %s: %w", t.host, err)
	}
	defer resp.Body.Close()
	// Read and dropped: an answer has nothing the instance acts on.
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxAnswerBytes))

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("%s answered with the status %d", t.host, resp.StatusCode)
	}
	return nil
}
