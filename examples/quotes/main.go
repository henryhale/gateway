// Command qoutes is a random quote generator that fans out across three
// unrelated third-party quote APIs through a single gw.Gateway. Every
// request to /quote is routed to one provider via round-robin; if that
// provider fails with a retryable error, the gateway falls back to another.
package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"time"

	gw "github.com/henryhale/gateway"
	"github.com/henryhale/gateway/examples/quotes/domain"
	"github.com/henryhale/gateway/examples/quotes/providers"
	"github.com/henryhale/gateway/httpgw"
)

func main() {
	if os.Getenv("QUOTES_API_ADDRESS") == "" {
		log.Fatalf("qoutes: QUOTES_API_ADDRESS not set")
	}

	quoteGateway, err := newQuoteGateway()
	if err != nil {
		log.Fatalf("qoutes: failed to build gateway: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/quote", quoteHandler(quoteGateway))

	server := &http.Server{
		Addr:              os.Getenv("QUOTES_API_ADDRESS"),
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	log.Printf("qoutes: random quote generator listening on %s", server.Addr)
	log.Printf("qoutes: try it with: curl http://localhost%s/quote", server.Addr)

	if err := server.ListenAndServe(); err != nil {
		log.Fatalf("qoutes: server stopped: %v", err)
	}
}

// newQuoteGateway registers the three quote providers and builds the gateway
// that fans requests out across them.
func newQuoteGateway() (*gw.Gateway, error) {
	client := &http.Client{Timeout: 5 * time.Second}
	zenQuotes, err := httpgw.NewProvider(client, providers.ZenQuotesCodec{})
	if err != nil {
		return nil, err
	}
	motivationalSpark, err := httpgw.NewProvider(client, providers.MotivationalSparkCodec{})
	if err != nil {
		return nil, err
	}
	textIntoImages, err := httpgw.NewProvider(client, providers.TextIntoImagesCodec{})
	if err != nil {
		return nil, err
	}

	return gw.New(
		gw.WithProviders(
			gw.UseProvider("zenquotes", zenQuotes, gw.WithOperations(domain.OperationRandomQuote)),
			gw.UseProvider("motivational-spark", motivationalSpark, gw.WithOperations(domain.OperationRandomQuote)),
			gw.UseProvider("textintoimages", textIntoImages, gw.WithOperations(domain.OperationRandomQuote)),
		),
		gw.WithRouting(gw.RoundRobin()),
		gw.WithFailurePolicy(gw.RetryThenFailover(
			1,
			gw.ExponentialBackoff{
				Initial: 100 * time.Millisecond,
				Maximum: time.Second,
			},
			providers.IsRetryable,
		)),
		gw.WithRequestTimeout(8*time.Second),
	)
}

// quoteHandler serves one random quote per request from whichever provider
// the gateway selects.
func quoteHandler(quoteGateway *gw.Gateway) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		result, err := quoteGateway.HandleRequest(
			r.Context(),
			gw.NewRequest(domain.OperationRandomQuote, domain.QuoteRequest{}),
		)
		if err != nil {
			http.Error(w, err.Error(), statusForError(err))
			return
		}
		quote, ok := gw.ValueAs[domain.Quote](result)
		if !ok {
			http.Error(w, "qoutes: provider returned an unexpected response", http.StatusBadGateway)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Quote-Provider", string(result.Provider()))
		_ = json.NewEncoder(w).Encode(quote)
	}
}

// statusForError maps a normalized gateway error to an HTTP status code.
func statusForError(err error) int {
	if statusCode, ok := providers.StatusCode(err); ok {
		switch statusCode {
		case http.StatusRequestTimeout, http.StatusGatewayTimeout:
			return http.StatusGatewayTimeout
		case http.StatusTooManyRequests:
			return http.StatusTooManyRequests
		}
	}

	gatewayError, ok := gw.AsError(err)
	if !ok {
		return http.StatusBadGateway
	}

	switch gatewayError.Code {
	case gw.CodeDeadlineExceeded:
		return http.StatusGatewayTimeout
	case gw.CodeInvalidRequest:
		return http.StatusBadRequest
	default:
		return http.StatusBadGateway
	}
}
