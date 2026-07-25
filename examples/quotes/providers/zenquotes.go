package providers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"examples/quotes/domain"

	gw "github.com/henryhale/gateway"
)

// ZenQuotesCodec translates between the standard quote types and the
// ZenQuotes API (https://zenquotes.io/api/random/).
type ZenQuotesCodec struct{}

// Supports reports whether this codec handles the operation.
func (ZenQuotesCodec) Supports(operation gw.Operation) bool {
	return operation == domain.OperationRandomQuote
}

// Encode builds the ZenQuotes request. It takes no parameters.
func (ZenQuotesCodec) Encode(_ context.Context, _ gw.Request[domain.QuoteRequest]) (gw.HTTPRequest, error) {
	return gw.HTTPRequest{Method: http.MethodGet, Path: "/api/random/"}, nil
}

// Decode translates the ZenQuotes response into a standard Quote.
//
// ZenQuotes returns a single-element array, e.g.:
//
//	[{"q": "...", "a": "...", "h": "..."}]
func (ZenQuotesCodec) Decode(_ context.Context, response gw.HTTPResponse) (domain.Quote, error) {
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return domain.Quote{}, gw.HTTPProviderError(response.StatusCode, "zenquotes_error", string(response.Body))
	}

	var quotes []struct {
		Text   string `json:"q"`
		Author string `json:"a"`
	}
	if err := json.Unmarshal(response.Body, &quotes); err != nil {
		return domain.Quote{}, err
	}
	if len(quotes) == 0 {
		return domain.Quote{}, errors.New("zenquotes: empty response")
	}

	return domain.Quote{
		Text:   quotes[0].Text,
		Author: quotes[0].Author,
		Source: "zen_quotes",
	}, nil
}
