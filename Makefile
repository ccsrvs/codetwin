.PHONY: build test test-verbose lint clean install run-example

BIN := codetwin
CMD := ./cmd/codetwin

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

lint:
	go vet ./...

clean:
	rm -f $(BIN) coverage.out coverage.html

# Run against the bundled testdata to verify the build works end-to-end
run-example: build
	./$(BIN) --threshold 0.3 ./testdata

run-example-json: build
	./$(BIN) --json --threshold 0.3 ./testdata

run-example-plain: build
	./$(BIN) --plain --threshold 0.3 ./testdata
