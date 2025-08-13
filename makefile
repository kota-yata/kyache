.PHONY: test example clean

test:
	go test ./...

example:
	go run example/main.go --http3

clean:
	go clean