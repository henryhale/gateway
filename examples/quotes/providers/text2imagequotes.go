package providers

import (
	"context"
	"encoding/json"
	"net/http"

	"examples/quotes/domain"

	gw "github.com/henryhale/gateway"
)

// TextIntoImagesCodec translates between the standard quote types and the
// Text Into Images API (https://textintoimages.com/random-quote/api/).
type TextIntoImagesCodec struct{}

// Supports reports whether this codec handles the operation.
func (TextIntoImagesCodec) Supports(operation gw.Operation) bool {
	return operation == domain.OperationRandomQuote
}

// Encode builds the Text Into Images request. It takes no parameters.
func (TextIntoImagesCodec) Encode(_ context.Context, _ gw.Request[domain.QuoteRequest]) (gw.HTTPRequest, error) {
	return gw.HTTPRequest{Method: http.MethodGet, Path: "/random-quote/api/"}, nil
}

// Decode translates the Text Into Images response into a standard Quote.
//
//	{"id": 886, "quoteText": "...", "quoteAuthor": "..."}
func (TextIntoImagesCodec) Decode(_ context.Context, response gw.HTTPResponse) (domain.Quote, error) {
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return domain.Quote{}, gw.HTTPProviderError(response.StatusCode, "textintoimages_error", string(response.Body))
	}

	var payload struct {
		QuoteText   string `json:"quoteText"`
		QuoteAuthor string `json:"quoteAuthor"`
	}
	if err := json.Unmarshal(response.Body, &payload); err != nil {
		return domain.Quote{}, err
	}

	author := payload.QuoteAuthor
	if author == "" {
		author = "Unknown"
	}

	return domain.Quote{
		Text:   payload.QuoteText,
		Author: author,
		Source: "text_to_images_quotes",
	}, nil
}
