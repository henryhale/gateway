package main

import (
	"context"
	"fmt"

	gw "github.com/henryhale/gateway"
)

type SMS struct {
	To   string
	Body string
}

type SMSResult struct{ MessageID string }

type SMSProvider struct{ prefix string }

// Handle sends a standard SMS request through one provider implementation.
func (p SMSProvider) Handle(_ context.Context, request gw.Request) (any, error) {
	sms := request.Value().(SMS)
	return SMSResult{MessageID: p.prefix + sms.To}, nil
}

// main demonstrates weighted routing across SMS providers.
func main() {
	gateway, err := gw.New(
		gw.WithProviders(
			gw.UseProvider(
				"provider-a",
				SMSProvider{prefix: "a-"},
				gw.WithOperations("sms.send"),
				gw.WithProviderWeight(70),
			),
			gw.UseProvider(
				"provider-b",
				SMSProvider{prefix: "b-"},
				gw.WithOperations("sms.send"),
				gw.WithProviderWeight(30),
			),
		),
		gw.WithRouting(gw.Weighted()),
	)
	if err != nil {
		panic(err)
	}

	result, err := gateway.HandleRequest(
		context.Background(),
		gw.NewRequest("sms.send", SMS{To: "+256700000000", Body: "hello"}),
	)
	if err != nil {
		panic(err)
	}
	response, _ := gw.ValueAs[SMSResult](result)
	fmt.Println(result.Provider(), response.MessageID)
}
