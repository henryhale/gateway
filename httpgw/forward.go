package httpgw

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"

	gateway "github.com/henryhale/gateway"
)

// ForwardProvider forwards *http.Request payloads to one upstream base URL.
//
// Handle returns *http.Response and intentionally leaves Response.Body open;
// the caller owns and must close it. ForwardProvider is useful for API routing
// where buffering would be undesirable.
type ForwardProvider struct {
	client  *http.Client
	baseURL *url.URL
}

// NewForwardProvider creates a streaming-friendly raw HTTP provider.
func NewForwardProvider(client *http.Client, baseURL string) (*ForwardProvider, error) {
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return nil, err
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return nil, errors.New("httpgw: base URL requires scheme and host")
	}
	if client == nil {
		client = http.DefaultClient
	}
	return &ForwardProvider{client: client, baseURL: parsed}, nil
}

// Handle forwards an HTTP request payload to the configured upstream.
func (p *ForwardProvider) Handle(ctx context.Context, request gateway.Request) (any, error) {
	incoming, ok := request.Value().(*http.Request)
	if !ok || incoming == nil {
		return nil, errors.New("httpgw: forward provider requires *http.Request payload")
	}
	outbound := incoming.Clone(ctx)
	outbound.RequestURI = ""
	outbound.URL.Scheme = p.baseURL.Scheme
	outbound.URL.Host = p.baseURL.Host
	outbound.URL.Path = joinPath(p.baseURL.Path, incoming.URL.Path)
	if p.baseURL.RawQuery != "" {
		if outbound.URL.RawQuery == "" {
			outbound.URL.RawQuery = p.baseURL.RawQuery
		} else {
			outbound.URL.RawQuery = p.baseURL.RawQuery + "&" + outbound.URL.RawQuery
		}
	}
	if incoming.GetBody != nil {
		body, err := incoming.GetBody()
		if err != nil {
			return nil, err
		}
		outbound.Body = body
	}
	return p.client.Do(outbound)
}

// joinPath joins an upstream base path and incoming request path.
func joinPath(basePath, requestPath string) string {
	if basePath == "" || basePath == "/" {
		if requestPath == "" {
			return "/"
		}
		return requestPath
	}
	return strings.TrimRight(basePath, "/") + "/" + strings.TrimLeft(requestPath, "/")
}
