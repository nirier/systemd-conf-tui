VERSION ?= 0.4.0

.PHONY: build test run release clean

build:
	go build -trimpath -ldflags "-X main.version=$(VERSION)" -o bin/systemd-conf-tui .

test:
	go test ./...
	go vet ./...

run:
	go run .

release:
	sh scripts/release.sh "$(VERSION)"

clean:
	rm -rf bin
