package webhook

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/fishtre-compagnie/husonym/internal/runevents"
)

const (
	// requestTimeout bounds one request as a whole: connection, sending, and the reading
	// of the answer.
	requestTimeout = 10 * time.Second
	// maxResponseBytes is how much of an answer is read. Reading an answer to its end
	// lets its connection serve the next delivery; past this size it is not worth it.
	maxResponseBytes = 64 << 10
)

// Delivery is one webhook to send.
type Delivery struct {
	// ID names the delivery of one event to one hook: every attempt carries the same, so
	// that a receiver can tell an attempt it has already processed.
	ID            string
	URL           string
	Secret        string
	SkipTLSVerify bool
	Event         *runevents.Event
}

// Sender delivers webhooks. It holds the HTTP connections to the receivers: one Sender
// serves a whole process and is safe for concurrent use.
type Sender struct {
	verified   route
	unverified route
	proxy      func(*http.Request) (*url.URL, error)
	now        func() time.Time
}

// route is the two ways to a receiver under one TLS mode. A connection made directly is
// checked by checkDestination. Through a proxy the connection is made to the proxy and the
// destination travels in the request, so that there is no address to check here: the proxy
// is where a deployment filters.
type route struct {
	direct  *http.Client
	proxied *http.Client
}

// NewSender returns a Sender that follows the proxy settings of the environment.
func NewSender() *Sender {
	return newSender(http.ProxyFromEnvironment, requestTimeout)
}

func newSender(proxy func(*http.Request) (*url.URL, error), timeout time.Duration) *Sender {
	newRoute := func(tlsConfig *tls.Config) route {
		return route{
			direct:  newClient(newTransport(nil, &net.Dialer{Timeout: timeout, Control: checkDestination}, tlsConfig), timeout),
			proxied: newClient(newTransport(proxy, &net.Dialer{Timeout: timeout}, tlsConfig), timeout),
		}
	}
	return &Sender{
		verified: newRoute(nil),
		// The owner of a hook may ask that the certificate of its receiver not be verified.
		unverified: newRoute(&tls.Config{InsecureSkipVerify: true}), //nolint:gosec // asked for by the hook
		proxy:      proxy,
		now:        time.Now,
	}
}

func newTransport(
	proxy func(*http.Request) (*url.URL, error),
	dialer *net.Dialer,
	tlsConfig *tls.Config,
) *http.Transport {
	return &http.Transport{
		Proxy:                  proxy,
		DialContext:            dialer.DialContext,
		TLSClientConfig:        tlsConfig,
		ForceAttemptHTTP2:      true, // a transport with its own dialer speaks HTTP/1.1 only otherwise
		MaxIdleConns:           100,
		IdleConnTimeout:        90 * time.Second,
		MaxResponseHeaderBytes: maxResponseBytes,
	}
}

func newClient(transport *http.Transport, timeout time.Duration) *http.Client {
	return &http.Client{
		Transport: transport,
		Timeout:   timeout,
		// A redirect is the answer, not a new destination: the body and its signatures are
		// for the receiver the hook names.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

// Send posts the event to the receiver. A nil error means the receiver answered with a 2xx
// status. Any other outcome is an *Error.
func (s *Sender) Send(ctx context.Context, d Delivery) error {
	target, failure := parseTarget(d.URL)
	if failure != nil {
		return failure
	}
	// The host is written by the owner of the hook: like everything an Error holds that
	// comes from outside, it is kept short and printable.
	host := excerpt([]byte(target.Host))
	if d.Event == nil {
		return &Error{Reason: ReasonEvent, Host: host, Detail: "there is no event"}
	}
	body, err := bodyOf(d.Event)
	if err != nil {
		return &Error{Reason: ReasonEvent, Host: host, Detail: excerpt([]byte(err.Error()))}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target.String(), bytes.NewReader(body))
	if err != nil {
		return &Error{Reason: ReasonInvalidURL, Host: host, Detail: "the request cannot be built"}
	}
	client, failure := s.client(req, d.SkipTLSVerify)
	if failure != nil {
		failure.Host = host
		return failure
	}
	timestamp := s.now().Unix()
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "husonym")
	req.Header.Set("X-Husonym-Signature", bodySignature(d.Secret, body))
	req.Header.Set("X-Husonym-Signature-Type", "sha256")
	req.Header.Set("webhook-id", d.ID)
	req.Header.Set("webhook-timestamp", strconv.FormatInt(timestamp, 10))
	req.Header.Set("webhook-signature", timestampedSignature(d.Secret, d.ID, timestamp, body))

	resp, err := client.Do(req)
	if err != nil {
		return transportError(host, err)
	}
	defer resp.Body.Close()
	answer, _ := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))

	reason, ok := reasonOfStatus(resp.StatusCode)
	if ok {
		return nil
	}
	detail := answer
	if reason == ReasonRedirected {
		// What matters of a redirect is where it points, not what its body says.
		detail = nil
		if location, err := resp.Location(); err == nil {
			detail = []byte("to " + location.Host)
		}
	}
	return &Error{Reason: reason, Status: resp.StatusCode, Host: host, Detail: excerpt(detail)}
}

// client picks the way to the receiver of the request.
func (s *Sender) client(req *http.Request, skipTLSVerify bool) (*http.Client, *Error) {
	way := s.verified
	if skipTLSVerify {
		way = s.unverified
	}
	proxy, err := s.proxy(req)
	if err != nil {
		// The error quotes the proxy setting, which may hold credentials: it is not kept.
		return nil, &Error{Reason: ReasonTransport, Detail: "the proxy setting of the worker is not valid"}
	}
	if proxy != nil {
		return way.proxied, nil
	}
	return way.direct, nil
}

// parseTarget accepts an http or https URL with a host, and nothing else.
func parseTarget(raw string) (*url.URL, *Error) {
	target, err := url.Parse(raw)
	if err != nil {
		// The error of the parser quotes the URL.
		return nil, &Error{Reason: ReasonInvalidURL, Detail: "it cannot be parsed"}
	}
	if target.Scheme != "http" && target.Scheme != "https" {
		return nil, &Error{Reason: ReasonInvalidURL, Detail: "the scheme is neither http nor https"}
	}
	if target.Hostname() == "" {
		return nil, &Error{Reason: ReasonInvalidURL, Detail: "it has no host"}
	}
	return target, nil
}

// transportError describes a request that got no answer. The error of the HTTP client
// quotes the URL: only the text of what it wraps is kept, short and printable, since part
// of it may come from what the request met on its way. The error itself is not kept as a
// cause, except the cancellation of the context, which a caller has to recognize.
func transportError(host string, err error) *Error {
	described := err
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		described = urlErr.Err
	}
	failure := &Error{Reason: ReasonTransport, Host: host, Detail: excerpt([]byte(described.Error()))}
	switch {
	case errors.Is(err, errDestinationRefused):
		failure.Reason = ReasonDestination
	case errors.Is(err, context.Canceled):
		failure.cause = context.Canceled
	}
	return failure
}
