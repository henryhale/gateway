package providers

import (
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
)

const maxErrorBodyBytes = 4 << 10

// HTTPError describes a non-successful response from a quote API.
type HTTPError struct {
	Status int
	Body   string
}

// Error returns a compact upstream failure description.
func (e *HTTPError) Error() string {
	if e.Body == "" {
		return fmt.Sprintf("quote provider returned HTTP %d", e.Status)
	}
	return fmt.Sprintf("quote provider returned HTTP %d: %s", e.Status, e.Body)
}

// IsRetryable reports whether a provider failure is safe to retry or fail over.
func IsRetryable(err error) bool {
	var httpError *HTTPError
	if errors.As(err, &httpError) {
		return httpError.Status == http.StatusRequestTimeout ||
			httpError.Status == http.StatusTooManyRequests ||
			httpError.Status >= http.StatusInternalServerError
	}
	var networkError net.Error
	return errors.As(err, &networkError)
}

// StatusCode extracts an upstream status code from an error chain.
func StatusCode(err error) (int, bool) {
	var httpError *HTTPError
	if !errors.As(err, &httpError) {
		return 0, false
	}
	return httpError.Status, true
}

// newHTTPError captures a bounded response body for diagnostics.
func newHTTPError(response *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(response.Body, maxErrorBodyBytes))
	return &HTTPError{Status: response.StatusCode, Body: strings.TrimSpace(string(body))}
}
