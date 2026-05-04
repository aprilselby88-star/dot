BINARY          := dot
GOFLAGS         := -trimpath
export CGO_ENABLED := 0

.PHONY: build install run vet clean

build:
	go build $(GOFLAGS) -o $(BINARY) .

install:
	go install $(GOFLAGS) .

run:
	go run .

vet:
	go vet ./...

clean:
	rm -f $(BINARY)
