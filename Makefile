BINARY  := littleclaw
OUTDIR  := bin
MODULE  := littleclaw

.PHONY: build vet test test-race test-cover install clean

build:
	go build -o $(OUTDIR)/$(BINARY) ./cmd/littleclaw/...

vet:
	go vet ./...

test:
	go test -v -count=1 ./...

test-race:
	go test -v -count=1 -race ./...

test-cover:
	go test -v -count=1 -coverprofile=coverage.out -coverpkg=$(MODULE)/pkg/... ./...
	go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: coverage.html"
	@echo "Coverage summary:"
	@go tool cover -func=coverage.out | grep total:

install: build
	cp $(OUTDIR)/$(BINARY) $(GOPATH)/bin/$(BINARY) 2>/dev/null || \
	cp $(OUTDIR)/$(BINARY) $(HOME)/go/bin/$(BINARY)

clean:
	rm -rf $(OUTDIR) coverage.out coverage.html
