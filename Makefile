BIN := argh
CMD := ./cmd/argh

.PHONY: build clean test run

build:
	go build -o $(BIN) $(CMD)

clean:
	go clean
	rm -f $(BIN) coverage*.out

test:
	go test -race -coverprofile=coverage.out ./...

run: build
	./$(BIN)
