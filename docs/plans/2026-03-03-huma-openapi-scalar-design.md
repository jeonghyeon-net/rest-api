# huma + OpenAPI 3.1 + Scalar UI 통합 설계

## 개요

기존 Fiber v3 REST API에 huma 프레임워크를 통합하여:
- OpenAPI 3.1 스펙 자동 생성
- Scalar UI로 인터랙티브 API 문서 제공
- 타입 안전한 Input/Output 기반 핸들러 패턴 도입

## 기술 선택

| 항목 | 선택 | 이유 |
|------|------|------|
| OpenAPI 생성 | huma v2 | 코드 = 스펙, 자동 동기화, OpenAPI 3.1 네이티브 |
| API 문서 UI | Scalar (huma 내장) | 모던 UI, CDN 기반, huma에 내장 |
| Fiber v3 연동 | 커스텀 어댑터 직접 구현 | huma 공식 어댑터가 Fiber v2만 지원 |

## 아키텍처

### Fiber v3 어댑터 (`internal/app/huma.go`)

huma의 `Adapter` 인터페이스(2개 메서드)와 `Context` 인터페이스(18개 메서드)를 구현.
Fiber v3의 `fiber.Ctx`를 감싸는 래퍼 패턴.

```
huma.Adapter interface:
  - Handle(op *Operation, handler func(ctx Context))  // 라우트 등록
  - ServeHTTP(http.ResponseWriter, *http.Request)      // 폴백 (Fiber에서는 미사용)

huma.Context interface:
  - Fiber v3 fiber.Ctx의 메서드를 1:1 매핑
  - Operation(), Context(), Method(), Host(), URL(), Param(), Query(),
    Header(), EachHeader(), BodyReader(), SetStatus(), Status(),
    SetHeader(), AppendHeader(), BodyWriter() 등
```

### 핸들러 마이그레이션

기존 Fiber 핸들러를 huma의 `Input/Output` struct + `huma.Register` 패턴으로 변환.

**변환 전 (Fiber 직접 사용):**
- `fiber.Ctx`에서 직접 파라미터 파싱, 바디 바인딩, 검증, 응답 전송
- `go-playground/validator` 수동 연동

**변환 후 (huma):**
- Input struct의 struct tag로 파라미터/바디/검증 선언
- Output struct로 응답 타입 선언
- huma가 파싱, 검증, 직렬화, OpenAPI 스펙 생성을 자동 처리

### 엔드포인트 목록 (11개)

| Method | Path | OperationID | Tags |
|--------|------|-------------|------|
| POST | /todos | create-todo | Todos |
| GET | /todos | list-todos | Todos |
| GET | /todos/{id} | get-todo | Todos |
| PATCH | /todos/{id} | update-todo | Todos |
| DELETE | /todos/{id} | delete-todo | Todos |
| POST | /todos/{id}/tags | add-todo-tag | Todos |
| DELETE | /todos/{id}/tags/{tagId} | remove-todo-tag | Todos |
| POST | /tags | create-tag | Tags |
| GET | /tags | list-tags | Tags |
| PATCH | /tags/{id} | update-tag | Tags |
| DELETE | /tags/{id} | delete-tag | Tags |

### 자동 노출 엔드포인트

| Path | 역할 |
|------|------|
| /docs | Scalar UI |
| /openapi.json | OpenAPI 3.1 JSON 스펙 |
| /openapi.yaml | OpenAPI 3.1 YAML 스펙 |

## 변경 파일

| 파일 | 변경 유형 | 설명 |
|------|-----------|------|
| `internal/app/huma.go` | 신규 | Fiber v3 huma 어댑터 |
| `internal/app/server.go` | 수정 | huma API 인스턴스 생성 + Scalar 설정 |
| `internal/app/module.go` | 수정 | huma API를 DI에 등록 |
| `internal/domain/todo/handler/http/handler.go` | 수정 | huma 패턴으로 전체 마이그레이션 |
| `cmd/server/main.go` | 수정 | 라우트 등록 방식 변경 |
| `go.mod` | 수정 | huma/v2 의존성 추가 |
| `internal/app/errors.go` | 수정 | huma 에러 핸들링과 통합 |
| E2E 테스트 | 수정 | huma 패턴에 맞게 테스트 업데이트 |

## 주의사항

- huma는 자체 유효성 검사를 내장하고 있어 기존 `go-playground/validator` 연동과 충돌 가능
  → huma의 검증을 사용하도록 전환
- Fiber의 경로 파라미터 문법 `:id<int>` → huma 문법 `{id}` 변환 필요
- huma의 에러 응답 형식이 기존 AppError와 다를 수 있음
  → huma의 에러 변환기(ErrorTransformer)로 기존 형식 유지
