# playa
BIN := bin/playa
URL ?= http://127.0.0.1:8080

.PHONY: build test vet check run tidy capture clean

build:
	go build -o $(BIN) ./cmd/playa

test:
	go test -race ./...

vet:
	go vet ./...

tidy:
	go mod tidy

# plain-text connectivity check against a running `laya serve` (no TUI)
check: build
	./$(BIN) -check -url $(URL)

run: build
	./$(BIN) -url $(URL)

# Save live responses to captures/ (needs curl and jq). Compare them with
# internal/api/testdata/ to spot server changes; copy over a fixture on purpose.
capture:
	mkdir -p captures
	curl -s $(URL)/health      | jq . > captures/health.json
	curl -s $(URL)/v1/models   | jq . > captures/models.json
	curl -s $(URL)/v1/presets  | jq . > captures/presets.json
	@echo "saved to captures/"

clean:
	rm -rf bin captures
