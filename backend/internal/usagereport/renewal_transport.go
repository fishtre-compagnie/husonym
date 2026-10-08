package usagereport

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/fishtre-compagnie/husonym/internal/telemetry"
)

// DefaultRenewalURL is the address the license that succeeds the one of the instance is asked
// at: the origin the usage report is sent to, at the path of a renewal.
const DefaultRenewalURL = "https://license.husonym.com" + telemetry.RenewalPath

// RenewalAsk is a request for the license that succeeds the one of the instance, with its seal.
type RenewalAsk struct {
	// Document is the exact JSON the seal is of, byte for byte.
	Document []byte
	// Seal proves that whoever made the document holds the license key.
	Seal string
	// KeyFingerprint designates the key the document is sealed with, without revealing it.
	KeyFingerprint string
}

// RenewalTransport posts one sealed request and gives the license that was answered, or an empty
// string when there is nothing to give. It never follows a redirect.
type RenewalTransport interface {
	Ask(ctx context.Context, ask *RenewalAsk) (string, error)
}

// HTTPRenewalTransport asks one address, over HTTP.
type HTTPRenewalTransport struct {
	sealed *sealedClient
}

// RenewalURL gives the address a license is asked at by an instance that sends its report to the
// given one: the same scheme, host and credentials, at the path of a renewal. The query and the
// fragment of the report address are for the report and are not kept. Its errors never quote the
// address, which may hold credentials.
func RenewalURL(reportAddress string) (string, error) {
	target, err := renewalTarget(reportAddress)
	if err != nil {
		return "", err
	}
	return target.String(), nil
}

func renewalTarget(reportAddress string) (*url.URL, error) {
	report, err := checkReportURL(reportAddress)
	if err != nil {
		return nil, err
	}
	target := *report
	target.Path = telemetry.RenewalPath
	target.RawPath = ""
	target.RawQuery = ""
	target.ForceQuery = false
	target.Fragment = ""
	target.RawFragment = ""
	return &target, nil
}

// NewHTTPRenewalTransport returns the transport of an instance that sends its report to the
// given address, which is one a report can be sent to. version is the version of this build.
func NewHTTPRenewalTransport(reportAddress, version string) (RenewalTransport, error) {
	target, err := renewalTarget(reportAddress)
	if err != nil {
		return nil, err
	}
	return newHTTPRenewalTransport(target, version, postTimeout), nil
}

func newHTTPRenewalTransport(target *url.URL, version string, timeout time.Duration) *HTTPRenewalTransport {
	return &HTTPRenewalTransport{sealed: newSealedClient(target, version, timeout)}
}

// Ask sends the request byte for byte, with its seal and the fingerprint of its key in headers.
// A 200 gives the license of the answer, a 204 nothing; any other status, a redirect among them,
// an answer larger than what is read of one and an answer that is not one are errors.
//
// An error is made of the host, of a status, and of words of this package and of the one that
// reads the answer: nothing the address, a proxy or the destination wrote is quoted in it.
func (t *HTTPRenewalTransport) Ask(ctx context.Context, ask *RenewalAsk) (string, error) {
	resp, err := t.sealed.post(ctx, ask.Document, ask.Seal, ask.KeyFingerprint)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		// Read and dropped: only the answer that carries a license is made anything of.
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, telemetry.RenewalAnswerCap))
		if resp.StatusCode == http.StatusNoContent {
			return "", nil
		}
		return "", t.sealed.answered(resp.StatusCode)
	}

	// One byte more than the bound tells an answer that is over it from one that fills it.
	body, err := io.ReadAll(io.LimitReader(resp.Body, telemetry.RenewalAnswerCap+1))
	if err != nil {
		return "", fmt.Errorf("unable to read the answer of %s: %s", t.sealed.host, failureOf(err))
	}
	if len(body) > telemetry.RenewalAnswerCap {
		return "", fmt.Errorf("%s answered with the status 200 and more than is read of an answer", t.sealed.host)
	}
	answer, err := telemetry.ParseRenewalAnswer(body)
	if err != nil {
		// The error of the reading is in fixed words and never quotes the body.
		return "", fmt.Errorf("%s answered with the status 200 and what is not an answer: %w", t.sealed.host, err)
	}
	return answer.License, nil
}
