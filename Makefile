.PHONY: fmt vet test race check

fmt:
	gofmt -w $$(find . -name '*.go')

vet:
	go vet ./...

test:
	go test ./...

race:
	go test -race ./...

check: fmt vet race

lint:
	golangci-lint config verify
	golangci-lint run ./...
	golangci-lint fmt ./...

