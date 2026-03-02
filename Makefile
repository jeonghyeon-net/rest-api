.PHONY: build run dev clean arch setup

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

setup:
	mise install
	git config core.hooksPath .githooks
	@echo "✅ 프로젝트 셋업 완료 (도구 설치 + Git hooks 활성화)"
