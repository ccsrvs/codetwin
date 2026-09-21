.PHONY: build test test-verbose test-coverage coverage-check benchmark performance-check lint clean install run-example

BIN := codetwin
CMD := ./cmd/codetwin
COVERAGE_MIN ?= 80.0
GOLANGCI_LINT_VERSION ?= v2.13.2

build:
	go build -o $(BIN) $(CMD)

install:
	go install $(CMD)

test:
	go test ./...

test-verbose:
	go test ./... -v

test-coverage:
	go test ./... -coverprofile=coverage.out
	go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: coverage.html"

coverage-check:
	go test ./... -race -coverprofile=coverage.out -covermode=atomic
	go tool cover -func=coverage.out > coverage.txt
	bash scripts/check-coverage.sh coverage.txt $(COVERAGE_MIN)

lint:
	go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION) run

benchmark:
	go test ./internal/similarity -run '^$$' -bench '^BenchmarkSimilarityStoragePipeline$$' -benchmem -count=5 > benchmark.txt
	cat benchmark.txt

performance-check: benchmark
	bash scripts/check-performance.sh benchmark.txt

clean:
	rm -f $(BIN) coverage.out coverage.txt coverage.html benchmark.txt

# Run against the bundled testdata to verify the build works end-to-end
run-example: build
	./$(BIN) --threshold 0.3 ./testdata

run-example-json: build
	./$(BIN) --json --threshold 0.3 ./testdata

run-example-plain: build
	./$(BIN) --plain --threshold 0.3 ./testdata
