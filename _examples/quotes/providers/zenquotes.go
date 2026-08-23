package providers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	gw "github.com/henryhale/gateway"
	"github.com/henryhale/gateway/_examples/quotes/domain"
)

// ZenQuotesCodec translates between the standard quote types and the
// ZenQuotes API (https://zenquotes.io/api/random/).
type ZenQuotesCodec struct{}

// Encode builds the ZenQuotes request. It takes no parameters.
func (ZenQuotesCodec) Encode(ctx context.Context, _ gw.Request) (*http.Request, error) {
	return http.NewRequestWithContext(ctx, http.MethodGet, "https://zenquotes.io/api/random/", nil)
}

// Decode translates the ZenQuotes response into a standard Quote.
//
// ZenQuotes returns a single-element array, e.g.:
//
//	[{"q": "...", "a": "...", "h": "..."}]
func (ZenQuotesCodec) Decode(_ context.Context, _ gw.Request, response *http.Response) (any, error) {
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, newHTTPError(response)
	}

	var quotes []struct {
		Text   string `json:"q"`
		Author string `json:"a"`
	}
	if err := json.NewDecoder(response.Body).Decode(&quotes); err != nil {
		return nil, err
	}
	if len(quotes) == 0 {
		return nil, errors.New("zenquotes: empty response")
	}

	return domain.Quote{
		Text:   quotes[0].Text,
		Author: quotes[0].Author,
		Source: "zen_quotes",
	}, nil
}
