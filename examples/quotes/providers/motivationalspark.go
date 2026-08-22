package providers

import (
	"context"
	"encoding/json"
	"net/http"

	gw "github.com/henryhale/gateway"
	"github.com/henryhale/gateway/examples/quotes/domain"
)

// MotivationalSparkCodec translates between the standard quote types and the
// Motivational Spark API (https://motivational-spark-api.vercel.app/api/quotes/random).
type MotivationalSparkCodec struct{}

// Encode builds the Motivational Spark request. It takes no parameters.
func (MotivationalSparkCodec) Encode(ctx context.Context, _ gw.Request) (*http.Request, error) {
	return http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		"https://motivational-spark-api.vercel.app/api/quotes/random",
		nil,
	)
}

// Decode translates the Motivational Spark response into a standard Quote.
//
//	{"author": "...", "quote": "..."}
func (MotivationalSparkCodec) Decode(_ context.Context, _ gw.Request, response *http.Response) (any, error) {
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, newHTTPError(response)
	}

	var payload struct {
		Author string `json:"author"`
		Quote  string `json:"quote"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return nil, err
	}

	return domain.Quote{
		Text:   payload.Quote,
		Author: payload.Author,
		Source: "motivational_spark_quotes",
	}, nil
}
