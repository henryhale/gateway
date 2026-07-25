package providers

import (
	"context"
	"encoding/json"
	"net/http"

	"examples/quotes/domain"

	gw "github.com/henryhale/gateway"
)

// MotivationalSparkCodec translates between the standard quote types and the
// Motivational Spark API (https://motivational-spark-api.vercel.app/api/quotes/random).
type MotivationalSparkCodec struct{}

// Supports reports whether this codec handles the operation.
func (MotivationalSparkCodec) Supports(operation gw.Operation) bool {
	return operation == domain.OperationRandomQuote
}

// Encode builds the Motivational Spark request. It takes no parameters.
func (MotivationalSparkCodec) Encode(_ context.Context, _ gw.Request[domain.QuoteRequest]) (gw.HTTPRequest, error) {
	return gw.HTTPRequest{Method: http.MethodGet, Path: "/api/quotes/random"}, nil
}

// Decode translates the Motivational Spark response into a standard Quote.
//
//	{"author": "...", "quote": "..."}
func (MotivationalSparkCodec) Decode(_ context.Context, response gw.HTTPResponse) (domain.Quote, error) {
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return domain.Quote{}, gw.HTTPProviderError(
			response.StatusCode,
			"motivational_spark_error",
			string(response.Body),
		)
	}

	var payload struct {
		Author string `json:"author"`
		Quote  string `json:"quote"`
	}
	if err := json.Unmarshal(response.Body, &payload); err != nil {
		return domain.Quote{}, err
	}

	return domain.Quote{
		Text:   payload.Quote,
		Author: payload.Author,
		Source: "motivational_spark_quotes",
	}, nil
}
