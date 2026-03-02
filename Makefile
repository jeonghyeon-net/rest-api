.PHONY: build run dev clean arch setup docker

build:
	go build -o tmp/main ./cmd/server

run: build
	./tmp/main

dev:
	air

arch:
	go test ./test/architecture -count=1

clean:
	rm -rf tmp

# Docker 이미지 빌드 (운영 배포용)
docker:
	docker build -t rest-api .

setup:
	mise install
	git config core.hooksPath .githooks
	@echo "✅ 프로젝트 셋업 완료 (도구 설치 + Git hooks 활성화)"
