BINARY  := littleclaw
OUTDIR  := bin
MODULE  := littleclaw

.PHONY: build vet test install clean

build:
	go build -o $(OUTDIR)/$(BINARY) ./cmd/littleclaw/...

vet:
	go vet ./...

test:
	go test ./...

install: build
	cp $(OUTDIR)/$(BINARY) $(GOPATH)/bin/$(BINARY) 2>/dev/null || \
	cp $(OUTDIR)/$(BINARY) $(HOME)/go/bin/$(BINARY)

clean:
	rm -rf $(OUTDIR)
