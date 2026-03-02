# ─────────────────────────────────────────────────────────────────────────────
# Makefile — 프로젝트의 모든 빌드·실행·품질 관리 명령을 하나로 모은 파일이다.
# ─────────────────────────────────────────────────────────────────────────────
#
# NestJS에서 package.json의 scripts 섹션과 같은 역할이다.
# 예: "npm run build", "npm run lint" 대신 "make build", "make lint"를 사용한다.
#
# .PHONY는 Make에게 "이 이름들은 파일이 아니라 명령어 이름"이라고 알려준다.
# 만약 프로젝트에 build라는 이름의 파일이 있으면, Make는 이미 최신이라고 판단하여
# 명령을 실행하지 않는다. .PHONY로 선언하면 항상 실행된다.
.PHONY: build run dev clean test arch setup docker fmt lint sqlc-gen migrate-new migrate-up migrate-down migrate-status

# ── 변수 ────────────────────────────────────────────────────────────────────

# 기본 DB 경로.
# ?= 연산자는 "환경변수가 이미 설정되어 있으면 그 값을 쓰고, 없으면 이 기본값을 쓴다"는 뜻이다.
# 사용 예: DB_PATH=./custom.db make migrate-up
DB_PATH ?= ./data/app.db

# ── 빌드 & 실행 ────────────────────────────────────────────────────────────

# Go 소스를 컴파일하여 실행 바이너리를 생성한다.
# NestJS의 npm run build (tsc 컴파일)와 같은 역할이다.
# -o tmp/main은 출력 파일 경로를 지정한다.
# ./cmd/server는 main 패키지가 있는 디렉터리다.
build:
	go build -o tmp/main ./cmd/server

# 빌드 후 바이너리를 직접 실행한다.
# NestJS의 npm run start (node dist/main.js)와 같은 역할이다.
# "run: build"에서 build는 의존 타겟(prerequisite)이다.
# Make는 run을 실행하기 전에 build를 먼저 실행한다.
run: build
	./tmp/main

# air를 사용한 핫 리로드 개발 서버를 시작한다.
# 파일이 변경되면 자동으로 재빌드·재시작한다.
# NestJS의 npm run start:dev (nest start --watch)와 같은 역할이다.
# air 설정은 .air.toml 파일에서 관리한다.
dev:
	air

# 빌드 산출물(tmp/ 디렉터리)을 삭제한다.
# NestJS의 npm run clean (rimraf dist)과 같은 역할이다.
clean:
	rm -rf tmp

# ── 코드 품질 ──────────────────────────────────────────────────────────────

# 코드 포맷팅을 자동 적용한다.
# gofmt는 Go 표준 포맷터, golangci-lint fmt는 추가 포맷 규칙(gofumpt 등)을 적용한다.
# NestJS의 npm run format (prettier --write)과 같은 역할이다.
fmt:
	gofmt -w .
	golangci-lint fmt

# 정적 분석(린트)을 실행하여 코드 품질 문제를 검출한다.
# golangci-lint는 30개 이상의 린터를 통합 실행하는 메타 린터다.
# nilaway는 Uber가 만든 nil 역참조(null pointer) 전용 정적 분석 도구다.
# NestJS의 npm run lint (eslint)와 같은 역할이다.
lint:
	golangci-lint run
	nilaway -exclude-errors-in-files="test/architecture" ./...

# 아키텍처 테스트를 제외한 모든 유닛 테스트를 실행한다.
# ./... 패턴은 현재 모듈의 모든 패키지를 의미한다.
# -count=1은 테스트 캐시를 무시하고 항상 새로 실행하게 한다.
# grep -v로 test/architecture 패키지를 제외한다 (아키텍처 검증은 make arch로 별도 실행).
# NestJS의 npm run test (jest)와 같은 역할이다.
test:
	go test $$(go list ./... | grep -v test/architecture) -count=1

# 아키텍처 규칙 준수 여부를 자동 검증한다.
# test/architecture/ 디렉터리의 테스트가 DDD 레이어 의존성, 네이밍 규칙,
# sqlc 코드 생성 규칙 등을 go/ast(추상 구문 트리)로 검사한다.
# NestJS에는 직접 대응하는 도구가 없지만, ArchUnit(Java)과 유사한 개념이다.
# -count=1은 테스트 캐시를 무시하고 항상 새로 실행하게 한다.
arch:
	go test ./test/architecture -count=1

# ── Docker ─────────────────────────────────────────────────────────────────

# Docker 이미지를 빌드한다.
# Dockerfile에 정의된 멀티스테이지 빌드를 실행한다.
# NestJS의 docker build와 동일하다.
docker:
	docker build -t rest-api .

# ── 프로젝트 초기 설정 ────────────────────────────────────────────────────

# 새로 프로젝트를 클론한 후 한 번 실행하는 초기 설정 명령이다.
# mise install: .mise.toml에 정의된 도구들(Go, golangci-lint, sqlc, goose 등)을 설치한다.
# git config core.hooksPath: Git 훅 디렉터리를 .githooks/로 지정하여
# 커밋 전 자동 포맷팅·린트를 활성화한다.
# NestJS의 npm install + husky install과 비슷한 역할이다.
setup:
	mise install
	git config core.hooksPath .githooks

# ── sqlc ───────────────────────────────────────────────────────────────────

# sqlc 코드 생성 — sqlc.yaml에 정의된 SQL 쿼리 파일들을 Go 코드로 변환한다.
# 생성된 Go 파일에는 타입 안전한 쿼리 함수가 포함된다.
# NestJS의 npx prisma generate와 비슷한 역할이다.
sqlc-gen:
	sqlc generate

# ── goose 마이그레이션 ─────────────────────────────────────────────────────

# 새 마이그레이션 파일을 생성한다.
# 사용법: make migrate-new NAME=create_users
# internal/db/migration/ 디렉터리에 타임스탬프가 붙은 SQL 파일이 생성된다.
# NestJS의 npx typeorm migration:create과 비슷하다.
migrate-new:
	goose -dir internal/db/migration create $(NAME) sql

# 아직 실행되지 않은 마이그레이션을 모두 적용한다.
# NestJS의 npx typeorm migration:run과 비슷하다.
migrate-up:
	goose -dir internal/db/migration sqlite3 $(DB_PATH) up

# 가장 최근 마이그레이션 1개를 되돌린다(롤백).
# NestJS의 npx typeorm migration:revert와 비슷하다.
migrate-down:
	goose -dir internal/db/migration sqlite3 $(DB_PATH) down

# 각 마이그레이션의 적용 상태를 보여준다.
# Applied/Pending 여부를 확인할 수 있다.
# NestJS의 npx typeorm migration:show와 비슷하다.
migrate-status:
	goose -dir internal/db/migration sqlite3 $(DB_PATH) status
