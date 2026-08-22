package gateway

import (
	"context"
	"testing"
)

// BenchmarkGatewaySingleProvider measures the minimal routing hot path.
func BenchmarkGatewaySingleProvider(b *testing.B) {
	provider := ProviderFunc(func(context.Context, Request) (any, error) { return nil, nil })
	g, err := New(WithProviders(UseProvider("p", provider)))
	if err != nil {
		b.Fatal(err)
	}
	ctx := context.Background()
	request := NewRequest("op", nil)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := g.HandleRequest(ctx, request); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkGatewayParallel measures concurrent routing through four providers.
func BenchmarkGatewayParallel(b *testing.B) {
	provider := ProviderFunc(func(context.Context, Request) (any, error) { return nil, nil })
	g, err := New(
		WithProviders(
			UseProvider("a", provider),
			UseProvider("b", provider),
			UseProvider("c", provider),
			UseProvider("d", provider),
		),
		WithRouting(PowerOfTwo(ByInFlight())),
	)
	if err != nil {
		b.Fatal(err)
	}
	ctx := context.Background()
	request := NewRequest("op", nil)
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			if _, err := g.HandleRequest(ctx, request); err != nil {
				b.Fatal(err)
			}
		}
	})
}
