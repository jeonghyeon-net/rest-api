# ---- 빌드 스테이지 ----
# 멀티 스테이지 빌드: 빌드 도구가 포함된 큰 이미지에서 바이너리를 만든 뒤,
# 최종 이미지에는 바이너리만 복사한다.
# NestJS의 npm run build → dist/ 만 배포하는 것과 같은 개념이다.
FROM golang:1.26-alpine AS builder

WORKDIR /app

# 의존성 캐싱: go.mod/go.sum만 먼저 복사하여 go mod download를 실행한다.
# 소스 코드가 바뀌어도 의존성이 동일하면 이 레이어는 Docker 캐시에서 재사용된다.
# NestJS에서 package.json만 먼저 COPY → npm install → 소스 COPY 하는 패턴과 동일하다.
COPY go.mod go.sum ./
RUN go mod download

# 전체 소스 코드 복사 후 빌드
COPY . .

# CGO_ENABLED=0: C 라이브러리 의존성 없이 순수 Go 바이너리를 만든다.
# 이렇게 하면 glibc가 없는 초경량 이미지(distroless)에서도 실행 가능하다.
RUN CGO_ENABLED=0 go build -o server ./cmd/server

# ---- 런타임 스테이지 ----
# distroless는 Google이 만든 초경량 컨테이너 이미지다.
# 셸, 패키지 매니저 등이 없어 공격 표면이 최소화된다.
# :nonroot 태그는 root가 아닌 사용자로 실행하여 보안을 강화한다.
FROM gcr.io/distroless/static:nonroot

# 빌드 스테이지에서 만든 바이너리만 복사한다.
# 최종 이미지에는 Go 컴파일러, 소스 코드 등이 포함되지 않는다.
COPY --from=builder /app/server /server

EXPOSE 42001

ENTRYPOINT ["/server"]
