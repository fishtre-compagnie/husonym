package usagereport

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/textproto"
	"net/url"
	"strings"
	"syscall"
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
	sealed *sealedClient
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
	return &HTTPTransport{sealed: newSealedClient(target, version, timeout)}
}

// Post sends the document of the report as it is stored, byte for byte, with its seal and the
// fingerprint of its key in headers. A nil error means the answer was a 2xx status.
//
// An error is made of the host, of a status, and of words of this file: nothing the address,
// a proxy or the destination wrote is quoted in it, whatever went wrong.
func (t *HTTPTransport) Post(ctx context.Context, report *usagestore.StoredReport) error {
	resp, err := t.sealed.post(ctx, report.Document, report.Seal, report.KeyFingerprint)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	// Read and dropped: an answer has nothing the instance acts on.
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxAnswerBytes))

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return t.sealed.answered(resp.StatusCode)
	}
	return nil
}

// sealedClient posts a sealed document to one address, over HTTP: the report of a day, and the
// request for the license that succeeds the one of the instance. It never follows a redirect.
type sealedClient struct {
	target *url.URL
	// host is how the address is named in an error: without its path nor its credentials.
	host      string
	userAgent string
	client    *http.Client
}

func newSealedClient(target *url.URL, version string, timeout time.Duration) *sealedClient {
	return &sealedClient{
		target:    target,
		host:      target.Host,
		userAgent: "husonym/" + telemetry.HusonymVersion(version),
		client: &http.Client{
			// A document leaves a few times a day at most: each request has its own connection.
			Transport: reportPolicy(target).Transport(timeout, false),
			Timeout:   timeout,
			// A redirect is the answer, not a new destination: the document and its seal are
			// for the address they are sent to.
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
	}
}

// post sends the document byte for byte, with its seal and the fingerprint of its key in
// headers, and gives the answer, whose body the caller reads within its bound and closes.
//
// An error is made of the host and of words of this file: nothing the address, a proxy or the
// destination wrote is quoted in it, whatever went wrong.
func (c *sealedClient) post(ctx context.Context, document []byte, seal, fingerprint string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.target.String(), bytes.NewReader(document))
	if err != nil {
		return nil, fmt.Errorf("unable to build the request to %s", c.host)
	}
	// Asked here first: the error of the client would quote the proxy setting, which may hold
	// credentials.
	if _, err := http.ProxyFromEnvironment(req); err != nil {
		return nil, errors.New("the proxy setting of the environment is not valid")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("Husonym-Seal", seal)
	req.Header.Set("Husonym-Key-Fingerprint", fingerprint)

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("unable to send to %s: %s", c.host, failureOf(err))
	}
	return resp, nil
}

// answered is the error of an answer whose status is not one that was hoped for.
func (c *sealedClient) answered(status int) error {
	return fmt.Errorf("%s answered with the status %d", c.host, status)
}

// What a request that got no answer is told as. The error of the client is never copied: it
// quotes the address, and it may quote what a proxy or the destination sent, at any length.
const (
	failureInterrupted = "the request was interrupted"
	failureTimedOut    = "timed out"
	failureTLS         = "TLS verification failed"
	failureUnresolved  = "name not resolved"
	failureRefused     = "connection refused"
	failureClosed      = "connection reset or closed"
	failureNotHTTP     = "not an HTTP answer"
	failureOther       = "the request failed"
)

// failureOf tells what went wrong in one of the fixed texts above, chosen by the kind of the
// error and never by what it says.
func failureOf(err error) string {
	var (
		timeout      net.Error
		verification *tls.CertificateVerificationError
		authority    x509.UnknownAuthorityError
		invalid      x509.CertificateInvalidError
		hostname     x509.HostnameError
		dns          *net.DNSError
		protocol     textproto.ProtocolError
		notTLS       tls.RecordHeaderError
	)
	switch {
	case errors.Is(err, context.Canceled):
		return failureInterrupted
	case errors.Is(err, context.DeadlineExceeded), errors.As(err, &timeout) && timeout.Timeout():
		return failureTimedOut
	case errors.As(err, &verification), errors.As(err, &authority), errors.As(err, &invalid), errors.As(err, &hostname):
		return failureTLS
	case errors.As(err, &dns):
		return failureUnresolved
	case errors.Is(err, syscall.ECONNREFUSED):
		return failureRefused
	case errors.Is(err, syscall.ECONNRESET), errors.Is(err, syscall.EPIPE), errors.Is(err, net.ErrClosed),
		errors.Is(err, io.EOF), errors.Is(err, io.ErrUnexpectedEOF):
		return failureClosed
	case errors.As(err, &protocol), errors.As(err, &notTLS):
		return failureNotHTTP
	}
	// The client tells of a first line that is not one of HTTP with a type it keeps to itself:
	// it is known by the words the client starts it with, which are its own, not the peer's.
	for cause := err; cause != nil; cause = errors.Unwrap(cause) {
		if strings.HasPrefix(cause.Error(), "malformed HTTP") {
			return failureNotHTTP
		}
	}
	return failureOther
}
