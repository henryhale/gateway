package httpgw

import (
	"context"
	"errors"
	"net/http"

	gateway "github.com/henryhale/gateway"
)

// Codec translates between an opaque standard gateway request and an HTTP API.
//
// Decode must fully consume any response body data it needs before returning;
// Provider closes the body after Decode returns.
type Codec interface {
	Encode(context.Context, gateway.Request) (*http.Request, error)
	Decode(context.Context, gateway.Request, *http.Response) (any, error)
}

// Provider executes translated HTTP APIs through a reusable http.Client.
type Provider struct {
	client *http.Client
	codec  Codec
}

// NewProvider creates a translated HTTP provider.
func NewProvider(client *http.Client, codec Codec) (*Provider, error) {
	if codec == nil {
		return nil, errors.New("httpgw: codec cannot be nil")
	}
	if client == nil {
		client = http.DefaultClient
	}
	return &Provider{client: client, codec: codec}, nil
}

// Handle translates, executes, and decodes one HTTP provider request.
func (p *Provider) Handle(ctx context.Context, request gateway.Request) (any, error) {
	outbound, err := p.codec.Encode(ctx, request)
	if err != nil {
		return nil, err
	}
	if outbound == nil {
		return nil, errors.New("httpgw: codec returned a nil HTTP request")
	}
	outbound = outbound.Clone(ctx)
	response, err := p.client.Do(outbound)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	return p.codec.Decode(ctx, request, response)
}
