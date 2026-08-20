package main

import (
	"context"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	gw "github.com/henryhale/gateway"
	"github.com/henryhale/gateway/httpgw"
)

// main demonstrates routing inbound HTTP requests without buffering upstream responses.
func main() {
	usersPrimary, err := httpgw.NewForwardProvider(nil, "http://127.0.0.1:9001")
	if err != nil {
		log.Fatal(err)
	}
	usersSecondary, err := httpgw.NewForwardProvider(nil, "http://127.0.0.1:9002")
	if err != nil {
		log.Fatal(err)
	}

	usersFilter := gw.FilterFunc(func(_ context.Context, request gw.Request, _ gw.Candidate) (bool, error) {
		httpRequest, ok := request.Value().(*http.Request)
		return ok && strings.HasPrefix(httpRequest.URL.Path, "/users"), nil
	})

	gateway, err := gw.New(
		gw.WithProviders(
			gw.UseProvider(
				"users-primary",
				usersPrimary,
				gw.WithOperations("http.proxy"),
				gw.WithFilter(usersFilter),
				gw.WithProviderPriority(1),
			),
			gw.UseProvider(
				"users-secondary",
				usersSecondary,
				gw.WithOperations("http.proxy"),
				gw.WithFilter(usersFilter),
				gw.WithProviderPriority(2),
			),
		),
		gw.WithRouting(gw.PowerOfTwo(gw.ByInFlight())),
	)
	if err != nil {
		log.Fatal(err)
	}

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		result, routeErr := gateway.HandleRequest(r.Context(), gw.NewRequest("http.proxy", r))
		if routeErr != nil {
			http.Error(w, routeErr.Error(), http.StatusBadGateway)
			return
		}
		upstream := result.Value().(*http.Response)
		defer upstream.Body.Close()
		for key, values := range upstream.Header {
			for _, value := range values {
				w.Header().Add(key, value)
			}
		}
		w.WriteHeader(upstream.StatusCode)
		_, _ = io.Copy(w, upstream.Body)
	})

	server := &http.Server{
		Addr:              ":8080",
		Handler:           http.DefaultServeMux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	log.Println("http: proxy listening on", server.Addr)
	log.Fatal(server.ListenAndServe())
}
