package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	gw "github.com/henryhale/gateway"
)

var errUnavailable = errors.New("provider unavailable")

type ProductQuery struct {
	SKU    string
	Region string
}

type ProductResult struct {
	SKU       string
	Name      string
	Available bool
}

type AvailabilityQuery struct {
	SKU    string
	Region string
}

type AvailabilityResult struct {
	SKU      string
	InStock  bool
	Quantity int
}

type CatalogService struct{ name string }

// Handle translates each standard catalog operation to a provider-specific action.
func (p CatalogService) Handle(_ context.Context, request gw.Request) (any, error) {
	switch request.Operation() {
	case "catalog.lookup":
		input := request.Value().(ProductQuery)
		return ProductResult{SKU: input.SKU, Name: p.name + " product", Available: true}, nil
	case "catalog.availability":
		input := request.Value().(AvailabilityQuery)
		return AvailabilityResult{SKU: input.SKU, InStock: true, Quantity: 25}, nil
	default:
		return nil, errors.New("unsupported operation")
	}
}

// main demonstrates a multi-operation catalog gateway with explicit failover.
func main() {
	gateway, err := gw.New(
		gw.WithProviders(
			gw.UseProvider(
				"catalog-primary",
				CatalogService{name: "primary"},
				gw.WithOperations("catalog.lookup", "catalog.availability"),
				gw.WithProviderPriority(1),
				gw.WithMaxInFlight(100),
				gw.WithCooldown(gw.CooldownConfig{
					Failures: 3,
					Duration: 5 * time.Second,
					When:     func(err error) bool { return errors.Is(err, errUnavailable) },
				}),
			),
			gw.UseProvider(
				"catalog-secondary",
				CatalogService{name: "secondary"},
				gw.WithOperations("catalog.lookup"),
				gw.WithProviderPriority(2),
			),
		),
		gw.WithRouting(gw.Priority("catalog-primary", "catalog-secondary")),
		gw.WithFailurePolicy(gw.FailoverWhen(func(err error) bool {
			return errors.Is(err, errUnavailable)
		})),
	)
	if err != nil {
		panic(err)
	}

	result, err := gateway.HandleRequest(context.Background(), gw.NewRequest(
		"catalog.lookup",
		ProductQuery{SKU: "SKU-1001", Region: "eu"},
	))
	if err != nil {
		panic(err)
	}

	product, ok := gw.ValueAs[ProductResult](result)
	if !ok {
		panic("unexpected result type")
	}
	fmt.Println(result.Provider(), product.SKU, product.Name, product.Available)
}
