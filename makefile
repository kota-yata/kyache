.PHONY: test example clean

test:
	go test ./...

example:
	go run example/main.go

clean:
	go clean