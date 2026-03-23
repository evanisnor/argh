BIN := argh
CMD := ./cmd/argh

.PHONY: build clean test run

build:
	go build -o $(BIN) $(CMD)

clean:
	go clean
	rm -f $(BIN) coverage*.out

test:
	go test -race -timeout 120s -coverprofile=coverage.out ./...

run: build
	./$(BIN)
