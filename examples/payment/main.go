package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	gw "github.com/henryhale/gateway"
)

var errUnavailable = errors.New("provider unavailable")

type Collection struct {
	Reference string
	Amount    int64
	Currency  string
}

type CollectionResult struct {
	TransactionID string
	Status        string
}

type BalanceRequest struct {
	Account string
}

type BalanceResult struct {
	Available int64
	Currency  string
}

type MobileMoney struct{ name string }

// Handle translates each standard payment operation to the provider-specific action.
func (p MobileMoney) Handle(_ context.Context, request gw.Request) (any, error) {
	switch request.Operation() {
	case "payment.collect":
		input := request.Value().(Collection)
		return CollectionResult{TransactionID: p.name + "-" + input.Reference, Status: "pending"}, nil
	case "payment.balance":
		return BalanceResult{Available: 250000, Currency: "UGX"}, nil
	default:
		return nil, errors.New("unsupported operation")
	}
}

// main demonstrates a multi-action payment gateway with safe explicit failover.
func main() {
	gateway, err := gw.New(
		gw.WithProviders(
			gw.UseProvider(
				"mtn",
				MobileMoney{name: "mtn"},
				gw.WithOperations("payment.collect", "payment.balance"),
				gw.WithProviderPriority(1),
				gw.WithMaxInFlight(100),
				gw.WithCooldown(gw.CooldownConfig{
					Failures: 3,
					Duration: 5 * time.Second,
					When:     func(err error) bool { return errors.Is(err, errUnavailable) },
				}),
			),
			gw.UseProvider(
				"airtel",
				MobileMoney{name: "airtel"},
				gw.WithOperations("payment.collect"),
				gw.WithProviderPriority(2),
			),
		),
		gw.WithRouting(gw.Priority("mtn", "airtel")),
		gw.WithFailurePolicy(gw.FailoverWhen(func(err error) bool {
			return errors.Is(err, errUnavailable)
		})),
	)
	if err != nil {
		panic(err)
	}

	result, err := gateway.HandleRequest(context.Background(), gw.NewRequest(
		"payment.collect",
		Collection{Reference: "ORDER-1001", Amount: 50000, Currency: "UGX"},
	))
	if err != nil {
		panic(err)
	}

	collection, ok := gw.ValueAs[CollectionResult](result)
	if !ok {
		panic("unexpected result type")
	}
	fmt.Println(result.Provider(), collection.TransactionID, collection.Status)
}
