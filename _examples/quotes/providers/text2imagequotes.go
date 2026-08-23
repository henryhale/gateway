package providers

import (
	"context"
	"encoding/json"
	"net/http"

	gw "github.com/henryhale/gateway"
	"github.com/henryhale/gateway/_examples/quotes/domain"
)

// TextIntoImagesCodec translates between the standard quote types and the
// Text Into Images API (https://textintoimages.com/random-quote/api/).
type TextIntoImagesCodec struct{}

// Encode builds the Text Into Images request. It takes no parameters.
func (TextIntoImagesCodec) Encode(ctx context.Context, _ gw.Request) (*http.Request, error) {
	return http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		"https://textintoimages.com/random-quote/api/",
		nil,
	)
}

// Decode translates the Text Into Images response into a standard Quote.
//
//	{"id": 886, "quoteText": "...", "quoteAuthor": "..."}
func (TextIntoImagesCodec) Decode(_ context.Context, _ gw.Request, response *http.Response) (any, error) {
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, newHTTPError(response)
	}

	var payload struct {
		QuoteText   string `json:"quoteText"`
		QuoteAuthor string `json:"quoteAuthor"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return nil, err
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
