package oidcprobe

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/fishtre-compagnie/husonym/backend/internal/safehttp"
)

// Trying a setting means fetching a URL somebody typed into a form. That is a request the
// server makes on behalf of an unprivileged caller, to a destination the caller chooses --
// a server-side request forgery unless it is bounded. Everything in this file is that
// bound.
const (
	// requestTimeout caps a single request. A provider that cannot answer a static
	// document in this time is a provider whose users would not get in either.
	requestTimeout = 5 * time.Second
	// totalTimeout caps the whole probe, redirects included, so a chain of slow hops
	// cannot hold a handler open.
	totalTimeout = 15 * time.Second
	// maxBodyBytes caps what is read. A discovery document is a few kilobytes; anything
	// larger is either not one or is trying to be a problem.
	maxBodyBytes = 1 << 20 // 1 MiB
	// maxRedirects is what a correctly configured provider needs, plus room for the
	// http-to-https and trailing-slash hops that real deployments have.
	maxRedirects = 5
)

// newSafeClient returns a client that only talks to where the policy lets it: by default a
// public https endpoint, checked at connection time for every hop (see safehttp).
func newSafeClient(policy safehttp.Policy) *http.Client {
	return &http.Client{
		Timeout: totalTimeout,
		// A connection per request: a probe is a few requests, now and then.
		Transport: policy.Transport(requestTimeout, false),
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= maxRedirects {
				return fmt.Errorf("stopped after %d redirects", maxRedirects)
			}
			return policy.CheckURL(req.URL)
		},
	}
}

// get fetches a URL under every bound in this file.
func get(ctx context.Context, client *http.Client, policy safehttp.Policy, rawURL string) (*http.Response, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("not a URL: %w", err)
	}
	if err := policy.CheckURL(parsed); err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), http.NoBody)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	return client.Do(req)
}
