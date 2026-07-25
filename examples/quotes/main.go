// Command qoutes is a random quote generator that fans out across three
// unrelated third-party quote APIs through a single gw.Gateway. Every
// request to /quote is routed to one provider via round-robin; if that
// provider fails with a retryable error, the gateway falls back to another.
package main

import (
	"encoding/json"
	"log"
	"log/slog"
	"net/http"
	"os"
	"time"

	"examples/quotes/domain"
	"examples/quotes/providers"

	gw "github.com/henryhale/gateway"
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
func newQuoteGateway() (*gw.Gateway[domain.QuoteRequest, domain.Quote], error) {
	zenQuotes := gw.NewHTTPProvider(
		"zenquotes",
		gw.HTTPProviderConfig{
			BaseURL: "https://zenquotes.io",
			Timeout: 5 * time.Second,
		},
		providers.ZenQuotesCodec{},
	)

	motivationalSpark := gw.NewHTTPProvider(
		"motivational-spark",
		gw.HTTPProviderConfig{
			BaseURL: "https://motivational-spark-api.vercel.app",
			Timeout: 5 * time.Second,
		},
		providers.MotivationalSparkCodec{},
	)

	textIntoImages := gw.NewHTTPProvider(
		"textintoimages",
		gw.HTTPProviderConfig{
			BaseURL: "https://textintoimages.com",
			Timeout: 5 * time.Second,
		},
		providers.TextIntoImagesCodec{},
	)

	return gw.New[domain.QuoteRequest, domain.Quote](
		gw.WithProviders(
			gw.UseProvider(zenQuotes, gw.WithProviderPriority(1)),
			gw.UseProvider(motivationalSpark, gw.WithProviderPriority(2)),
			gw.UseProvider(textIntoImages, gw.WithProviderPriority(3)),
		),
		gw.WithRouting(gw.RoundRobin()),
		gw.WithFallback(gw.FallbackOn(
			gw.ErrorTimeout,
			gw.ErrorUnavailable,
			gw.ErrorRateLimited,
		)),
		gw.WithRetry(gw.Retry{
			MaxAttempts: 2,
			Backoff: gw.ExponentialBackoff{
				Initial: 100 * time.Millisecond,
				Maximum: time.Second,
				Jitter:  0.2,
			},
		}),
		gw.WithRequestTimeout(8*time.Second),
		gw.WithLogger(slog.Default()),
	)
}

// quoteHandler serves one random quote per request from whichever provider
// the gateway selects.
func quoteHandler(quoteGateway *gw.Gateway[domain.QuoteRequest, domain.Quote]) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		result, err := quoteGateway.HandleRequest(r.Context(), gw.Request[domain.QuoteRequest]{
			Operation: domain.OperationRandomQuote,
		})
		if err != nil {
			http.Error(w, err.Error(), statusForError(err))
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Quote-Provider", result.Provider)
		_ = json.NewEncoder(w).Encode(result.Payload)
	}
}

// statusForError maps a normalized gateway error to an HTTP status code.
func statusForError(err error) int {
	gatewayError, ok := gw.AsError(err)
	if !ok {
		return http.StatusBadGateway
	}

	switch gatewayError.Kind {
	case gw.ErrorTimeout:
		return http.StatusGatewayTimeout
	case gw.ErrorRateLimited:
		return http.StatusTooManyRequests
	case gw.ErrorValidation:
		return http.StatusBadRequest
	default:
		return http.StatusBadGateway
	}
}
