.PHONY: build run dev clean

build:
	go build -o tmp/main .

run: build
	./tmp/main

dev:
	air

clean:
	rm -rf tmp
