.PHONY: fmt vet test race bench check lint

fmt:
	gofmt -w $$(find . -name '*.go' -type f)

vet:
	go vet ./...

test:
	go test ./...

race:
	go test -race ./...

bench:
	go test -run '^$$' -bench . -benchmem ./...

check: fmt vet test race

lint:
	golangci-lint run --timeout 2m