.PHONY: build run dev clean arch

build:
	go build -o tmp/main .

run: build
	./tmp/main

dev:
	air

arch:
	go test ./test/architecture -count=1

clean:
	rm -rf tmp
