.PHONY: build run dev clean arch setup docker fmt lint sqlc-gen migrate-new migrate-up migrate-down migrate-status

# 기본 DB 경로 (환경변수로 오버라이드 가능: DB_PATH=./custom.db make migrate-up)
DB_PATH ?= ./data/app.db

build:
	go build -o tmp/main ./cmd/server

run: build
	./tmp/main

dev:
	air

arch:
	go test ./test/architecture -count=1

fmt:
	gofmt -w .
	golangci-lint fmt

lint:
	golangci-lint run
	nilaway -exclude-errors-in-files="test/architecture" ./...

clean:
	rm -rf tmp

docker:
	docker build -t rest-api .

setup:
	mise install
	git config core.hooksPath .githooks

# ── sqlc ──

# sqlc 코드 생성 — query.sql 파일을 Go 코드로 변환한다.
# NestJS의 prisma generate와 비슷한 역할이다.
sqlc-gen:
	sqlc generate

# ── goose 마이그레이션 ──

# 새 마이그레이션 파일 생성 (사용법: make migrate-new NAME=create_users)
# NestJS의 typeorm migration:create과 비슷하다.
migrate-new:
	goose -dir internal/db/migration create $(NAME) sql

# 마이그레이션 적용 — 아직 실행되지 않은 마이그레이션을 모두 적용한다.
migrate-up:
	goose -dir internal/db/migration sqlite3 $(DB_PATH) up

# 마이그레이션 롤백 — 가장 최근 마이그레이션 1개를 되돌린다.
migrate-down:
	goose -dir internal/db/migration sqlite3 $(DB_PATH) down

# 마이그레이션 상태 확인 — 각 마이그레이션의 적용 여부를 보여준다.
migrate-status:
	goose -dir internal/db/migration sqlite3 $(DB_PATH) status
