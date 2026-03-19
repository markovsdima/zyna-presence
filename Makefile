.PHONY: build run clean

build:
	go build -o bin/zyna-presence ./cmd/server

run: build
	./bin/zyna-presence

clean:
	rm -rf bin/
