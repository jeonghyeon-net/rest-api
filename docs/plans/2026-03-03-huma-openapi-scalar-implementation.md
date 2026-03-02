# huma + OpenAPI 3.1 + Scalar UI 구현 계획

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** 기존 Fiber v3 REST API에 huma를 통합하여 OpenAPI 3.1 자동 생성 + Scalar UI 문서를 제공한다.

**Architecture:** Fiber v3 커스텀 어댑터를 작성하여 huma를 연결한다. 기존 핸들러를 huma의 Input/Output struct + huma.Register 패턴으로 마이그레이션한다. Scalar UI는 huma 내장 기능을 사용한다.

**Tech Stack:** huma v2, Fiber v3 (기존), Scalar (huma 내장), fx DI (기존)

---

### Task 1: huma 의존성 추가

**Files:**
- Modify: `go.mod`

**Step 1: huma v2 패키지 설치**

```bash
cd /Users/me/Desktop/rest-api && go get github.com/danielgtaylor/huma/v2
```

**Step 2: 의존성 정리**

```bash
go mod tidy
```

**Step 3: 설치 확인**

```bash
grep "danielgtaylor/huma" go.mod
```

Expected: `github.com/danielgtaylor/huma/v2 v2.x.x`

**Step 4: 빌드 확인**

```bash
make build
```

Expected: 성공

**Step 5: 커밋**

```bash
git add go.mod go.sum
git commit -m "huma v2 의존성 추가"
```

---

### Task 2: Fiber v3 huma 어댑터 작성

**Files:**
- Create: `internal/app/huma.go`

**참고:** 기존 humafiber v2 어댑터(github.com/danielgtaylor/huma/v2/adapters/humafiber)를 Fiber v3 API에 맞게 포팅한다.

**Fiber v2 → v3 핵심 차이점:**
- `*fiber.Ctx`(포인터) → `fiber.Ctx`(인터페이스)
- `fiber.Handler` = `func(fiber.Ctx) error`
- `c.Context()` → Go `context.Context` 반환 (v2에서는 fasthttp RequestCtx 반환)
- `c.RequestCtx()` → fasthttp `*RequestCtx` 반환 (v3 전용)
- `router.Add(method string, ...)` → `router.Add(methods []string, ...)`

**Step 1: 어댑터 파일 작성**

`internal/app/huma.go` 전체 코드:

```go
// Package app은 huma와 Fiber v3를 연결하는 어댑터를 제공한다.
//
// huma는 라우터에 독립적인(router-agnostic) API 프레임워크로,
// huma.Adapter 인터페이스를 구현하면 어떤 HTTP 라우터든 사용할 수 있다.
// 공식 어댑터가 Fiber v2만 지원하므로, Fiber v3 어댑터를 직접 구현한다.
//
// NestJS에서 Fastify와 Express 어댑터를 교체할 수 있는 것과 같은 개념이다.
// NestJS의 @nestjs/platform-fastify가 Fastify 어댑터인 것처럼,
// 이 파일이 Fiber v3 어댑터 역할을 한다.
package app

import (
	"bytes"
	"context"
	"crypto/tls"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/gofiber/fiber/v3"
)

// ──────────────────────────────────────────────────────────────────────────────
// huma.Context 구현: fiberCtx
// ──────────────────────────────────────────────────────────────────────────────
//
// fiberCtx는 Fiber v3의 fiber.Ctx를 huma.Context 인터페이스로 감싸는 래퍼(wrapper)다.
// huma는 이 인터페이스를 통해 HTTP 요청 정보를 읽고 응답을 작성한다.
//
// Go의 인터페이스 구현 패턴:
// huma.Context가 요구하는 ~20개의 메서드를 모두 구현해야 한다.
// 각 메서드는 Fiber v3의 대응되는 메서드를 1:1로 호출하므로 복잡한 로직은 없다.

// fiberCtx는 huma.Context를 구현하는 Fiber v3 래퍼다.
type fiberCtx struct {
	op     *huma.Operation // 현재 요청이 매칭된 OpenAPI 오퍼레이션 정보
	orig   fiber.Ctx       // 원본 Fiber 컨텍스트 (v3에서는 인터페이스)
	status int             // 응답 상태 코드 (huma가 관리)
}

// 컴파일 타임에 huma.Context 인터페이스 구현을 검증한다.
// Go에서 인터페이스 충족 여부를 빌드 시점에 확인하는 관용 패턴이다.
// 메서드가 하나라도 빠지면 컴파일 에러가 발생한다.
var _ huma.Context = (*fiberCtx)(nil)

// Unwrap은 원본 Fiber 컨텍스트를 반환한다.
// Fiber 전용 기능(예: c.Locals, c.IP)이 필요할 때 사용한다.
//
// 주의: fasthttp의 제로 할당 특성상, 핸들러 밖에서 컨텍스트를 참조하면 안 된다.
func (c *fiberCtx) Unwrap() fiber.Ctx {
	return c.orig
}

// Operation은 현재 요청이 매칭된 OpenAPI 오퍼레이션 정보를 반환한다.
// huma가 내부적으로 요청 검증, 응답 직렬화 시 이 정보를 사용한다.
func (c *fiberCtx) Operation() *huma.Operation {
	return c.op
}

// Matched는 매칭된 라우트 패턴을 반환한다.
// 예: "/todos/{id}" → 실제 요청 URL이 아닌 등록된 패턴을 반환한다.
// Fiber v3에서 c.Route()는 매칭된 라우트 정보를 담은 *fiber.Route를 반환한다.
func (c *fiberCtx) Matched() string {
	return c.orig.Route().Path
}

// Context는 Go 표준 context.Context를 반환한다.
// 핸들러에서 서비스 레이어로 컨텍스트를 전달할 때 사용한다.
//
// Fiber v3에서 c.Context()는 Go 표준 context.Context를 반환한다.
// (Fiber v2에서는 fasthttp.RequestCtx를 반환했으나 v3에서 변경됨)
func (c *fiberCtx) Context() context.Context {
	return c.orig.Context()
}

// Method는 HTTP 메서드(GET, POST, PATCH 등)를 반환한다.
func (c *fiberCtx) Method() string {
	return c.orig.Method()
}

// Host는 요청의 호스트(예: "localhost:8080")를 반환한다.
func (c *fiberCtx) Host() string {
	return c.orig.Hostname()
}

// RemoteAddr는 클라이언트의 원격 주소(IP:Port)를 반환한다.
// Fiber v3에서 fasthttp 컨텍스트에 접근하려면 c.RequestCtx()를 사용한다.
func (c *fiberCtx) RemoteAddr() string {
	return c.orig.RequestCtx().RemoteAddr().String()
}

// URL은 전체 요청 URL을 반환한다.
// fasthttp의 RequestURI()는 []byte를 반환하므로 string으로 변환 후 파싱한다.
func (c *fiberCtx) URL() url.URL {
	u, _ := url.Parse(string(c.orig.Request().RequestURI()))
	return *u
}

// Param은 경로 파라미터 값을 반환한다.
// 예: /todos/{id}에서 Param("id") → "123"
// Fiber의 c.Params()와 동일한 역할이다.
func (c *fiberCtx) Param(name string) string {
	return c.orig.Params(name)
}

// Query는 쿼리 파라미터 값을 반환한다.
// 예: /todos?page=2에서 Query("page") → "2"
func (c *fiberCtx) Query(name string) string {
	return c.orig.Query(name)
}

// Header는 요청 헤더 값을 반환한다.
// Fiber의 c.Get()은 HTTP 요청 헤더를 조회한다 (응답 헤더가 아님).
func (c *fiberCtx) Header(name string) string {
	return c.orig.Get(name)
}

// EachHeader는 모든 요청 헤더를 순회하며 콜백을 호출한다.
// huma가 내부적으로 Content-Type, Accept 등을 탐색할 때 사용한다.
//
// fasthttp의 VisitAll은 Go 표준 http.Header.Range()와 같은 역할이다.
// 키와 값이 []byte로 전달되므로 string()으로 변환한다.
func (c *fiberCtx) EachHeader(cb func(name, value string)) {
	c.orig.Request().Header.VisitAll(func(key, value []byte) {
		cb(string(key), string(value))
	})
}

// BodyReader는 요청 바디를 io.Reader로 반환한다.
// huma가 JSON 파싱 시 이 Reader에서 바디를 읽는다.
//
// Fiber의 c.Body()는 이미 읽힌 바디를 []byte로 반환하므로,
// bytes.NewReader로 io.Reader 인터페이스를 만든다.
func (c *fiberCtx) BodyReader() io.Reader {
	return bytes.NewReader(c.orig.Body())
}

// GetMultipartForm은 멀티파트 폼 데이터를 반환한다.
// 파일 업로드 등에 사용되며, 이 프로젝트에서는 현재 미사용이다.
func (c *fiberCtx) GetMultipartForm() (*multipart.Form, error) {
	return c.orig.MultipartForm()
}

// SetReadDeadline은 요청 바디 읽기에 대한 데드라인을 설정한다.
// 대용량 요청이나 스트리밍에서 타임아웃을 제어할 때 사용한다.
func (c *fiberCtx) SetReadDeadline(deadline time.Time) error {
	return c.orig.RequestCtx().Conn().SetReadDeadline(deadline)
}

// SetStatus는 HTTP 응답 상태 코드를 설정한다.
// huma가 핸들러 실행 후 적절한 상태 코드를 설정할 때 호출한다.
func (c *fiberCtx) SetStatus(code int) {
	c.status = code
	c.orig.Status(code)
}

// Status는 현재 설정된 응답 상태 코드를 반환한다.
func (c *fiberCtx) Status() int {
	return c.status
}

// AppendHeader는 응답 헤더에 값을 추가한다.
// 같은 헤더에 여러 값이 있을 수 있을 때 사용한다 (예: Set-Cookie).
func (c *fiberCtx) AppendHeader(name, value string) {
	c.orig.Append(name, value)
}

// SetHeader는 응답 헤더를 설정한다 (기존 값을 덮어씀).
func (c *fiberCtx) SetHeader(name, value string) {
	c.orig.Set(name, value)
}

// BodyWriter는 응답 바디를 쓸 수 있는 io.Writer를 반환한다.
// huma가 JSON 응답을 직렬화하여 이 Writer에 쓴다.
//
// Fiber v3에서 fasthttp.RequestCtx는 io.Writer를 구현하므로,
// c.RequestCtx()를 그대로 반환한다.
func (c *fiberCtx) BodyWriter() io.Writer {
	return c.orig.RequestCtx()
}

// TLS는 TLS/SSL 연결 정보를 반환한다. HTTPS가 아니면 nil이다.
func (c *fiberCtx) TLS() *tls.ConnectionState {
	return c.orig.RequestCtx().TLSConnectionState()
}

// Version은 HTTP 프로토콜 버전을 반환한다.
// 예: "HTTP/1.1", "HTTP/2"
func (c *fiberCtx) Version() huma.ProtoVersion {
	return huma.ProtoVersion{
		Proto: c.orig.Protocol(),
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// huma.Adapter 구현: fiberAdapter
// ──────────────────────────────────────────────────────────────────────────────
//
// fiberAdapter는 huma.Adapter 인터페이스를 구현하여
// huma가 Fiber v3에 라우트를 등록하고 요청을 처리할 수 있게 한다.
//
// huma.Adapter는 두 개의 메서드만 요구한다:
//   - Handle: huma 오퍼레이션을 라우터에 등록
//   - ServeHTTP: 표준 http.Handler로 동작 (OpenAPI 스펙 서빙에 사용)

// fiberRouter는 Fiber v3 라우터의 Add 메서드만 추상화한 인터페이스다.
// *fiber.App과 fiber.Router(그룹) 모두 이 인터페이스를 만족한다.
type fiberRouter interface {
	Add(methods []string, path string, handlers ...fiber.Handler) fiber.Router
}

// fiberTester는 Fiber v3 앱의 Test 메서드를 추상화한 인터페이스다.
// ServeHTTP에서 *http.Request를 Fiber로 전달할 때 사용한다.
type fiberTester interface {
	Test(req *http.Request, msTimeout ...int) (*http.Response, error)
}

// fiberAdapter는 huma.Adapter를 구현하는 Fiber v3 어댑터다.
type fiberAdapter struct {
	router fiberRouter  // 라우트 등록용 (앱 또는 그룹)
	tester fiberTester  // HTTP 테스트용 (항상 앱)
}

// Handle은 huma 오퍼레이션을 Fiber 라우트로 등록한다.
// huma가 huma.Register()를 호출하면 내부적으로 이 메서드가 실행된다.
//
// huma의 경로 파라미터 문법 {param}을 Fiber의 :param으로 변환한다.
// 예: "/todos/{id}" → "/todos/:id"
func (a *fiberAdapter) Handle(op *huma.Operation, handler func(huma.Context)) {
	// huma 경로 문법을 Fiber 경로 문법으로 변환한다.
	// huma: {param}, Fiber: :param
	// NestJS에서 @Param('id')가 Express의 :id와 매핑되는 것과 비슷하다.
	path := op.Path
	path = strings.ReplaceAll(path, "{", ":")
	path = strings.ReplaceAll(path, "}", "")

	// Fiber 라우터에 핸들러를 등록한다.
	// Fiber v3에서 Add의 첫 번째 인자는 메서드 슬라이스다.
	a.router.Add([]string{op.Method}, path, func(c fiber.Ctx) error {
		// Fiber 컨텍스트를 huma 컨텍스트로 감싸서 핸들러에 전달한다.
		handler(&fiberCtx{
			op:   op,
			orig: c,
		})
		// huma가 응답을 직접 작성하므로 Fiber에는 nil(에러 없음)을 반환한다.
		return nil
	})
}

// ServeHTTP는 표준 http.Handler 인터페이스를 구현한다.
// huma가 OpenAPI 스펙 JSON/YAML, Scalar UI 등을 서빙할 때 사용한다.
// Fiber의 Test() 메서드로 *http.Request를 처리하고 결과를 http.ResponseWriter에 복사한다.
func (a *fiberAdapter) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	resp, err := a.tester.Test(r)
	if resp != nil && resp.Body != nil {
		defer func() {
			_ = resp.Body.Close()
		}()
	}

	if err != nil {
		panic(err)
	}

	// Fiber의 응답 헤더를 표준 http.ResponseWriter로 복사한다.
	h := w.Header()
	for k, v := range resp.Header {
		for _, item := range v {
			h.Add(k, item)
		}
	}

	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}

// ──────────────────────────────────────────────────────────────────────────────
// 생성자 함수
// ──────────────────────────────────────────────────────────────────────────────

// NewHumaAPI는 Fiber v3 앱을 감싸서 huma.API 인스턴스를 생성한다.
// 모든 huma 오퍼레이션이 이 API 인스턴스에 등록된다.
//
// NestJS에서 SwaggerModule.setup()으로 Swagger를 앱에 마운트하는 것과 같다.
func NewHumaAPI(app *fiber.App, cfg huma.Config) huma.API {
	return huma.NewAPI(cfg, &fiberAdapter{router: app, tester: app})
}

// NewHumaAPIWithGroup은 Fiber v3 그룹(route group)에 huma를 마운트한다.
// 예: /api/v1 그룹 아래에 모든 오퍼레이션을 등록할 때 사용한다.
func NewHumaAPIWithGroup(app *fiber.App, group fiber.Router, cfg huma.Config) huma.API {
	return huma.NewAPI(cfg, &fiberAdapter{router: group, tester: app})
}

// UnwrapFiberCtx는 huma.Context에서 원본 Fiber 컨텍스트를 추출한다.
// huma 미들웨어에서 Fiber 전용 기능이 필요할 때 사용한다.
//
// 다른 어댑터(예: chi, echo)의 컨텍스트가 전달되면 panic이 발생한다.
func UnwrapFiberCtx(ctx huma.Context) fiber.Ctx {
	// huma 미들웨어가 컨텍스트를 래핑할 수 있으므로, Unwrap 체인을 따라간다.
	for {
		if c, ok := ctx.(interface{ Unwrap() huma.Context }); ok {
			ctx = c.Unwrap()
			continue
		}

		break
	}

	if c, ok := ctx.(*fiberCtx); ok {
		return c.Unwrap()
	}

	panic("humafiber: 호환되지 않는 huma.Context 타입")
}
```

**Step 2: 빌드 확인**

```bash
make build
```

Expected: 성공 (아직 사용하는 곳이 없으므로 unused import 경고 가능 → 다음 태스크에서 연결)

**Step 3: 커밋**

```bash
git add internal/app/huma.go
git commit -m "Fiber v3 huma 어댑터 구현"
```

---

### Task 3: AppError에 huma StatusError 인터페이스 추가

**Files:**
- Modify: `internal/app/errors.go`

**배경:**
huma는 핸들러가 반환한 에러에서 `GetStatus() int` 메서드를 찾아 HTTP 상태 코드를 결정한다.
기존 AppError에 이 메서드를 추가하면 huma가 자동으로 올바른 상태 코드를 사용한다.

**Step 1: GetStatus 메서드 추가**

`internal/app/errors.go`에 추가:

```go
// GetStatus는 huma의 StatusError 인터페이스를 구현한다.
// huma는 핸들러에서 반환된 에러에 이 메서드가 있으면
// 반환값을 HTTP 응답 상태 코드로 사용한다.
//
// 이 메서드 덕분에 기존 서비스 레이어의 AppError가
// huma 핸들러에서도 그대로 동작한다.
func (e *AppError) GetStatus() int {
	return e.Status
}
```

**Step 2: 빌드 확인**

```bash
make build
```

Expected: 성공

**Step 3: 커밋**

```bash
git add internal/app/errors.go
git commit -m "AppError에 huma StatusError 인터페이스 구현"
```

---

### Task 4: huma API 인스턴스를 DI에 통합 + Scalar 설정

**Files:**
- Modify: `internal/app/server.go`
- Modify: `internal/app/module.go`

**Step 1: server.go에 huma API 생성 함수 추가**

`internal/app/server.go`의 `newFiberApp` 함수 아래에 추가:

```go
// newHumaConfig는 huma API 설정을 생성한다.
// OpenAPI 문서의 기본 정보와 Scalar UI 설정을 포함한다.
//
// NestJS에서 SwaggerModule.setup(app, {
//   title: 'REST API',
//   version: '1.0.0',
//   ...
// })으로 Swagger 설정을 하는 것과 같다.
func newHumaConfig() huma.Config {
	config := huma.DefaultConfig("REST API", "1.0.0")

	// Scalar를 API 문서 UI로 사용한다.
	// huma는 Scalar, Stoplight Elements, Swagger UI를 내장 지원한다.
	// Scalar는 모던한 디자인과 OpenAPI 3.1 완벽 지원이 장점이다.
	config.DocsRenderer = huma.DocsRendererScalar

	// /docs 경로에서 Scalar UI를 제공한다.
	// NestJS에서 SwaggerModule.setup(app, { path: 'docs' })과 같다.
	config.DocsPath = "/docs"

	// /openapi 접두사로 스펙 파일을 제공한다.
	// /openapi.json, /openapi.yaml 경로가 자동 생성된다.
	config.OpenAPIPath = "/openapi"

	// JSON Schema 경로
	config.SchemasPath = "/schemas"

	return config
}

// newHumaAPI는 Fiber 앱을 감싸서 huma API 인스턴스를 생성한다.
// fx.Provide에 등록하여 DI 컨테이너가 huma.API를 자동 주입하게 한다.
//
// 반환 타입이 huma.API(인터페이스)인 점에 주의.
// 외부에서 구현 세부사항(fiberAdapter)에 의존하지 않게 된다.
func newHumaAPI(app *fiber.App) huma.API {
	return NewHumaAPI(app, newHumaConfig())
}
```

server.go의 import에 huma 추가:

```go
import (
	// ... 기존 imports ...
	"github.com/danielgtaylor/huma/v2"
)
```

**Step 2: module.go에 huma API provider 등록**

`internal/app/module.go`의 AppModule() 함수에 추가:

```go
func AppModule() fx.Option {
	return fx.Options(
		fx.Provide(newLogger),
		fx.Provide(newFiberApp),
		fx.Provide(db.NewDB),

		// huma API 인스턴스를 DI에 등록한다.
		// huma.API 타입이 필요한 곳(핸들러의 RegisterRoutes)에 자동 주입된다.
		// newHumaAPI는 *fiber.App을 받아 huma.API를 반환하므로,
		// fx가 위에서 등록한 *fiber.App을 자동으로 주입한다.
		//
		// NestJS에서 SwaggerModule을 imports에 등록하는 것과 같다.
		fx.Provide(newHumaAPI),
	)
}
```

**Step 3: 빌드 확인**

```bash
make build
```

Expected: 성공

**Step 4: 커밋**

```bash
git add internal/app/server.go internal/app/module.go
git commit -m "huma API 인스턴스 DI 통합 및 Scalar UI 설정"
```

---

### Task 5: 핸들러를 huma 패턴으로 마이그레이션

**Files:**
- Modify: `internal/domain/todo/handler/http/handler.go`

**배경:**
이것이 가장 큰 변경이다. 기존 11개 Fiber 핸들러를 huma의 Input/Output struct + huma.Register 패턴으로 전환한다.

**핵심 변경점:**
1. `RegisterRoutes(app *fiber.App)` → `RegisterRoutes(api huma.API)`
2. 기존 req 구조체 → huma Input struct (경로/쿼리 파라미터 + Body 포함)
3. 기존 핸들러 시그니처 `func(fiber.Ctx) error` → `func(context.Context, *Input) (*Output, error)`
4. 수동 파라미터 파싱/검증 코드 제거 (huma가 자동 처리)
5. 경로 문법 변경: `:id<int>` → `{id}` (huma 문법, 어댑터가 Fiber 문법으로 변환)
6. `paramInt`, `queryInt` 헬퍼 제거 (huma가 Input struct에서 자동 파싱)

**Step 1: handler.go 전체를 huma 패턴으로 재작성**

**주의:** 이 파일은 전체 재작성이다. 주석 정책(한국어, Go 초심자 대상)을 유지한다.

`internal/domain/todo/handler/http/handler.go` 전체 코드:

```go
// Package http는 Todo 도메인의 HTTP 핸들러를 제공한다.
// huma 프레임워크를 사용하여 OpenAPI 3.1 스펙을 자동 생성하는 REST API 엔드포인트를 등록한다.
//
// huma는 Input/Output 구조체의 태그에서 OpenAPI 스펙을 자동으로 추출하므로,
// 코드가 곧 API 문서가 된다 (Code-first 접근법).
//
// NestJS에서 @Controller() + @ApiTags() + @ApiOperation()으로
// Swagger 문서를 생성하는 것과 같은 역할이다.
//
// 3-tuple 패턴을 따른다:
//   - 공개 인터페이스(Handler) — 외부에 노출되는 계약
//   - 비공개 구현체(handler)   — 실제 요청 처리 로직을 담은 구조체
//   - 생성자 함수(New)         — 구현체를 생성하여 인터페이스로 반환
package http

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"rest-api/internal/app"
	"rest-api/internal/domain/todo"
	"rest-api/internal/domain/todo/subdomain/core/model"
	tagmodel "rest-api/internal/domain/todo/subdomain/tag/model"
)

// ──────────────────────────────────────────────────────────────────────────────
// huma Input/Output 구조체 정의
// ──────────────────────────────────────────────────────────────────────────────
//
// huma는 Input/Output 구조체의 태그에서 OpenAPI 스펙을 자동 추출한다.
// NestJS에서 DTO에 @ApiProperty()를 붙이는 것과 같은 역할이다.
//
// Input 구조체 태그 규칙:
//   - path:"id"              → URL 경로 파라미터 (항상 필수)
//   - query:"page"           → 쿼리 파라미터 (기본 선택)
//   - Body struct { ... }    → 요청 바디 (필드는 기본 필수)
//   - required:"true"        → 필수 필드 표시
//   - doc:"설명"              → OpenAPI description
//   - minimum:"1"            → 최솟값
//   - maximum:"100"          → 최댓값
//   - minLength:"1"          → 문자열 최소 길이
//   - maxLength:"200"        → 문자열 최대 길이
//   - default:"20"           → 기본값
//
// Output 구조체 규칙:
//   - Body 필드가 JSON 응답 바디로 직렬화됨

// ─── Todo CRUD ───────────────────────────────────────────────────────────────

// CreateTodoInput은 할 일 생성 요청의 입력 구조체다.
// NestJS의 CreateTodoDto + @ApiBody()와 같다.
type CreateTodoInput struct {
	Body struct {
		Title string `json:"title" required:"true" minLength:"1" maxLength:"200" doc:"할 일 제목"`
		Body  string `json:"body" maxLength:"5000" doc:"할 일 내용" default:""`
	}
}

// CreateTodoOutput은 할 일 생성 응답의 출력 구조체다.
type CreateTodoOutput struct {
	Body model.TodoWithTags
}

// ListTodosInput은 할 일 목록 조회 요청의 입력 구조체다.
// 쿼리 파라미터로 페이지네이션과 태그 필터를 지원한다.
type ListTodosInput struct {
	Page  int    `query:"page" default:"1" minimum:"1" doc:"페이지 번호"`
	Limit int    `query:"limit" default:"20" minimum:"1" maximum:"100" doc:"페이지당 항목 수"`
	Tag   string `query:"tag" doc:"태그 이름으로 필터링"`
}

// ListTodosOutput은 할 일 목록 조회 응답의 출력 구조체다.
type ListTodosOutput struct {
	Body model.TodoList
}

// IDParam은 경로 파라미터에서 ID를 추출하는 공통 구조체다.
// Go의 구조체 임베딩으로 여러 Input에서 재사용한다.
// NestJS에서 @Param('id', ParseIntPipe)와 같다.
type IDParam struct {
	ID int64 `path:"id" doc:"리소스 ID"`
}

// GetTodoInput은 할 일 단건 조회 요청의 입력 구조체다.
type GetTodoInput struct {
	IDParam
}

// GetTodoOutput은 할 일 단건 조회 응답의 출력 구조체다.
type GetTodoOutput struct {
	Body model.TodoWithTags
}

// UpdateTodoInput은 할 일 수정 요청의 입력 구조체다.
type UpdateTodoInput struct {
	IDParam
	Body struct {
		Title string `json:"title" required:"true" minLength:"1" maxLength:"200" doc:"할 일 제목"`
		Body  string `json:"body" maxLength:"5000" doc:"할 일 내용"`
		Done  bool   `json:"done" doc:"완료 여부"`
	}
}

// UpdateTodoOutput은 할 일 수정 응답의 출력 구조체다.
type UpdateTodoOutput struct {
	Body model.TodoWithTags
}

// DeleteTodoInput은 할 일 삭제 요청의 입력 구조체다.
type DeleteTodoInput struct {
	IDParam
}

// ─── Todo-Tag 연결 ───────────────────────────────────────────────────────────

// AddTodoTagInput은 할 일에 태그를 연결하는 요청의 입력 구조체다.
type AddTodoTagInput struct {
	IDParam
	Body struct {
		TagID int64 `json:"tagId" required:"true" minimum:"1" doc:"연결할 태그 ID"`
	}
}

// RemoveTodoTagInput은 할 일에서 태그를 해제하는 요청의 입력 구조체다.
type RemoveTodoTagInput struct {
	ID    int64 `path:"id" doc:"할 일 ID"`
	TagID int64 `path:"tagId" doc:"태그 ID"`
}

// ─── Tag CRUD ────────────────────────────────────────────────────────────────

// CreateTagInput은 태그 생성 요청의 입력 구조체다.
type CreateTagInput struct {
	Body struct {
		Name string `json:"name" required:"true" minLength:"1" maxLength:"50" doc:"태그 이름"`
	}
}

// CreateTagOutput은 태그 생성 응답의 출력 구조체다.
type CreateTagOutput struct {
	Body tagmodel.Tag
}

// ListTagsOutput은 태그 목록 조회 응답의 출력 구조체다.
type ListTagsOutput struct {
	Body []tagmodel.Tag
}

// UpdateTagInput은 태그 수정 요청의 입력 구조체다.
type UpdateTagInput struct {
	IDParam
	Body struct {
		Name string `json:"name" required:"true" minLength:"1" maxLength:"50" doc:"태그 이름"`
	}
}

// UpdateTagOutput은 태그 수정 응답의 출력 구조체다.
type UpdateTagOutput struct {
	Body tagmodel.Tag
}

// DeleteTagInput은 태그 삭제 요청의 입력 구조체다.
type DeleteTagInput struct {
	IDParam
}

// ──────────────────────────────────────────────────────────────────────────────
// Handler 인터페이스 + 구현체
// ──────────────────────────────────────────────────────────────────────────────

// Handler는 Todo 도메인의 HTTP 핸들러 인터페이스다.
// RegisterRoutes로 huma API에 오퍼레이션을 등록한다.
//
// 기존에는 *fiber.App을 받았지만, huma 도입으로 huma.API를 받도록 변경되었다.
// huma.API에 오퍼레이션을 등록하면 OpenAPI 스펙이 자동으로 생성된다.
type Handler interface {
	RegisterRoutes(api huma.API)
}

// handler는 Handler 인터페이스의 비공개 구현체다.
type handler struct {
	svc todo.Service
}

// New는 Todo HTTP 핸들러를 생성한다.
func New(svc todo.Service) Handler {
	return &handler{svc: svc}
}

// RegisterRoutes는 Todo 도메인의 모든 HTTP 오퍼레이션을 huma API에 등록한다.
//
// huma.Register()는 Operation 메타데이터와 핸들러 함수를 받아
// 라우트 등록 + OpenAPI 스펙 생성을 동시에 수행한다.
//
// 기존 Fiber의 app.Group() + todos.Get("/") 패턴 대신
// huma.Register(api, Operation{Path: "/todos"}, handler) 패턴을 사용한다.
//
// NestJS에서 @Get(), @Post() 데코레이터 + @ApiOperation()으로
// 라우트와 Swagger 문서를 동시에 등록하는 것과 같다.
func (h *handler) RegisterRoutes(api huma.API) {
	// ─── Todo CRUD ────────────────────────────────────────────────────────
	huma.Register(api, huma.Operation{
		OperationID:   "create-todo",
		Method:        http.MethodPost,
		Path:          "/todos",
		Summary:       "할 일 생성",
		Description:   "새로운 할 일을 생성한다. 생성 직후에는 태그가 없으므로 빈 태그 목록과 함께 반환한다.",
		Tags:          []string{"Todos"},
		DefaultStatus: http.StatusCreated,
	}, h.createTodo)

	huma.Register(api, huma.Operation{
		OperationID: "list-todos",
		Method:      http.MethodGet,
		Path:        "/todos",
		Summary:     "할 일 목록 조회",
		Description: "페이지네이션된 할 일 목록을 반환한다. 태그 이름으로 필터링할 수 있다.",
		Tags:        []string{"Todos"},
	}, h.listTodos)

	huma.Register(api, huma.Operation{
		OperationID: "get-todo",
		Method:      http.MethodGet,
		Path:        "/todos/{id}",
		Summary:     "할 일 단건 조회",
		Description: "ID로 할 일을 조회하고, 연결된 태그 목록도 함께 반환한다.",
		Tags:        []string{"Todos"},
	}, h.getTodo)

	huma.Register(api, huma.Operation{
		OperationID: "update-todo",
		Method:      http.MethodPatch,
		Path:        "/todos/{id}",
		Summary:     "할 일 수정",
		Description: "할 일의 제목, 내용, 완료 여부를 수정한다.",
		Tags:        []string{"Todos"},
	}, h.updateTodo)

	huma.Register(api, huma.Operation{
		OperationID:   "delete-todo",
		Method:        http.MethodDelete,
		Path:          "/todos/{id}",
		Summary:       "할 일 삭제",
		Description:   "할 일을 삭제한다. 연결된 태그 관계(todo_tags)는 CASCADE로 자동 삭제된다.",
		Tags:          []string{"Todos"},
		DefaultStatus: http.StatusNoContent,
	}, h.deleteTodo)

	// ─── Todo-Tag 연결 ───────────────────────────────────────────────────
	huma.Register(api, huma.Operation{
		OperationID:   "add-todo-tag",
		Method:        http.MethodPost,
		Path:          "/todos/{id}/tags",
		Summary:       "할 일에 태그 연결",
		Description:   "할 일에 태그를 연결한다. 이미 연결된 태그를 다시 연결하면 409 Conflict를 반환한다.",
		Tags:          []string{"Todos"},
		DefaultStatus: http.StatusNoContent,
	}, h.addTodoTag)

	huma.Register(api, huma.Operation{
		OperationID:   "remove-todo-tag",
		Method:        http.MethodDelete,
		Path:          "/todos/{id}/tags/{tagId}",
		Summary:       "할 일에서 태그 해제",
		Description:   "할 일에서 태그 연결을 해제한다.",
		Tags:          []string{"Todos"},
		DefaultStatus: http.StatusNoContent,
	}, h.removeTodoTag)

	// ─── Tag CRUD ─────────────────────────────────────────────────────────
	huma.Register(api, huma.Operation{
		OperationID:   "create-tag",
		Method:        http.MethodPost,
		Path:          "/tags",
		Summary:       "태그 생성",
		Description:   "새로운 태그를 생성한다. 태그 이름은 고유해야 하며, 중복 시 409 Conflict를 반환한다.",
		Tags:          []string{"Tags"},
		DefaultStatus: http.StatusCreated,
	}, h.createTag)

	huma.Register(api, huma.Operation{
		OperationID: "list-tags",
		Method:      http.MethodGet,
		Path:        "/tags",
		Summary:     "태그 목록 조회",
		Description: "전체 태그 목록을 반환한다. 태그는 수가 적으므로 페이지네이션 없이 전체를 반환한다.",
		Tags:        []string{"Tags"},
	}, h.listTags)

	huma.Register(api, huma.Operation{
		OperationID: "update-tag",
		Method:      http.MethodPatch,
		Path:        "/tags/{id}",
		Summary:     "태그 수정",
		Description: "태그 이름을 수정한다.",
		Tags:        []string{"Tags"},
	}, h.updateTag)

	huma.Register(api, huma.Operation{
		OperationID:   "delete-tag",
		Method:        http.MethodDelete,
		Path:          "/tags/{id}",
		Summary:       "태그 삭제",
		Description:   "태그를 삭제한다. 이 태그와 연결된 todo_tags 레코드는 CASCADE로 자동 삭제된다.",
		Tags:          []string{"Tags"},
		DefaultStatus: http.StatusNoContent,
	}, h.deleteTag)
}

// ──────────────────────────────────────────────────────────────────────────────
// Todo 핸들러 메서드
// ──────────────────────────────────────────────────────────────────────────────
//
// huma 핸들러 시그니처: func(ctx context.Context, input *Input) (*Output, error)
//
// 기존 Fiber 핸들러와 비교:
//   - 기존: func(c fiber.Ctx) error → 직접 파싱, 검증, 응답 전송
//   - huma: func(ctx, input) (output, error) → huma가 파싱/검증/직렬화 자동 처리
//
// huma에서 nil을 반환하면:
//   - (*Output)(nil), nil → DefaultStatus로 응답 (바디 없음, 204 등에 적합)
//   - output, nil → 200(또는 DefaultStatus)으로 output.Body를 JSON 응답
//   - nil, error → 에러 응답 (GetStatus()가 있으면 해당 상태 코드, 없으면 500)

// createTodo는 새로운 할 일을 생성한다.
// POST /todos
func (h *handler) createTodo(ctx context.Context, input *CreateTodoInput) (*CreateTodoOutput, error) {
	result, err := h.svc.CreateTodo(ctx, input.Body.Title, input.Body.Body)
	if err != nil {
		return nil, fmt.Errorf("할 일 생성 실패: %w", err)
	}

	return &CreateTodoOutput{Body: result}, nil
}

// listTodos는 할 일 목록을 조회한다.
// GET /todos?page=1&limit=20&tag=urgent
func (h *handler) listTodos(ctx context.Context, input *ListTodosInput) (*ListTodosOutput, error) {
	result, err := h.svc.ListTodos(ctx, input.Page, input.Limit, input.Tag)
	if err != nil {
		return nil, fmt.Errorf("할 일 목록 조회 실패: %w", err)
	}

	return &ListTodosOutput{Body: result}, nil
}

// getTodo는 특정 할 일을 ID로 조회한다.
// GET /todos/{id}
func (h *handler) getTodo(ctx context.Context, input *GetTodoInput) (*GetTodoOutput, error) {
	result, err := h.svc.GetTodo(ctx, input.ID)
	if err != nil {
		return nil, fmt.Errorf("할 일 조회 실패: %w", err)
	}

	return &GetTodoOutput{Body: result}, nil
}

// updateTodo는 할 일을 수정한다.
// PATCH /todos/{id}
func (h *handler) updateTodo(ctx context.Context, input *UpdateTodoInput) (*UpdateTodoOutput, error) {
	result, err := h.svc.UpdateTodo(ctx, input.ID, input.Body.Title, input.Body.Body, input.Body.Done)
	if err != nil {
		return nil, fmt.Errorf("할 일 수정 실패: %w", err)
	}

	return &UpdateTodoOutput{Body: result}, nil
}

// deleteTodo는 할 일을 삭제한다.
// DELETE /todos/{id}
//
// (*Output)(nil), nil을 반환하면 huma가 DefaultStatus(204)로 응답한다.
func (h *handler) deleteTodo(ctx context.Context, input *DeleteTodoInput) (*struct{}, error) {
	if err := h.svc.DeleteTodo(ctx, input.ID); err != nil {
		return nil, fmt.Errorf("할 일 삭제 실패: %w", err)
	}

	return nil, nil
}

// ──────────────────────────────────────────────────────────────────────────────
// Todo-Tag 연결 핸들러 메서드
// ──────────────────────────────────────────────────────────────────────────────

// addTodoTag는 할 일에 태그를 연결한다.
// POST /todos/{id}/tags
func (h *handler) addTodoTag(ctx context.Context, input *AddTodoTagInput) (*struct{}, error) {
	if err := h.svc.AddTodoTag(ctx, input.ID, input.Body.TagID); err != nil {
		return nil, fmt.Errorf("할 일-태그 연결 실패: %w", err)
	}

	return nil, nil
}

// removeTodoTag는 할 일에서 태그 연결을 해제한다.
// DELETE /todos/{id}/tags/{tagId}
func (h *handler) removeTodoTag(ctx context.Context, input *RemoveTodoTagInput) (*struct{}, error) {
	if err := h.svc.RemoveTodoTag(ctx, input.ID, input.TagID); err != nil {
		return nil, fmt.Errorf("할 일-태그 연결 해제 실패: %w", err)
	}

	return nil, nil
}

// ──────────────────────────────────────────────────────────────────────────────
// Tag 핸들러 메서드
// ──────────────────────────────────────────────────────────────────────────────

// createTag는 새로운 태그를 생성한다.
// POST /tags
func (h *handler) createTag(ctx context.Context, input *CreateTagInput) (*CreateTagOutput, error) {
	result, err := h.svc.CreateTag(ctx, input.Body.Name)
	if err != nil {
		return nil, fmt.Errorf("태그 생성 실패: %w", err)
	}

	return &CreateTagOutput{Body: result}, nil
}

// listTags는 전체 태그 목록을 조회한다.
// GET /tags
func (h *handler) listTags(ctx context.Context, _ *struct{}) (*ListTagsOutput, error) {
	result, err := h.svc.ListTags(ctx)
	if err != nil {
		return nil, fmt.Errorf("태그 목록 조회 실패: %w", err)
	}

	return &ListTagsOutput{Body: result}, nil
}

// updateTag는 태그 이름을 수정한다.
// PATCH /tags/{id}
func (h *handler) updateTag(ctx context.Context, input *UpdateTagInput) (*UpdateTagOutput, error) {
	result, err := h.svc.UpdateTag(ctx, input.ID, input.Body.Name)
	if err != nil {
		return nil, fmt.Errorf("태그 수정 실패: %w", err)
	}

	return &UpdateTagOutput{Body: result}, nil
}

// deleteTag는 태그를 삭제한다.
// DELETE /tags/{id}
func (h *handler) deleteTag(ctx context.Context, input *DeleteTagInput) (*struct{}, error) {
	if err := h.svc.DeleteTag(ctx, input.ID); err != nil {
		return nil, fmt.Errorf("태그 삭제 실패: %w", err)
	}

	return nil, nil
}

// wrapError는 서비스 에러를 huma 에러로 변환하는 헬퍼다.
// AppError가 아닌 일반 에러는 500 Internal Server Error로 변환한다.
//
// 참고: AppError는 GetStatus() 메서드를 구현하므로
// huma가 자동으로 상태 코드를 추출한다.
// 이 함수는 AppError가 아닌 에러를 처리할 때 사용한다.
func wrapError(err error) error {
	var appErr *app.AppError
	if errors.As(err, &appErr) {
		// AppError는 GetStatus()를 구현하므로 huma가 자동 처리한다.
		return appErr
	}

	// 예상치 못한 에러는 500으로 반환한다.
	return huma.Error500InternalServerError("서버 내부 오류가 발생했습니다", err)
}
```

**Step 2: 빌드 확인**

```bash
make build
```

Expected: 빌드 성공 (main.go가 아직 이전 방식이므로 huma.API 관련 에러 가능 → Task 6에서 수정)

**Step 3: 린트 확인 (경고 확인)**

```bash
make lint
```

**Step 4: 커밋**

```bash
git add internal/domain/todo/handler/http/handler.go
git commit -m "Todo 핸들러를 huma Input/Output 패턴으로 마이그레이션"
```

---

### Task 6: main.go 라우트 등록 방식 변경

**Files:**
- Modify: `cmd/server/main.go`

**Step 1: main.go에서 huma.API 사용하도록 변경**

변경할 부분 — fx.Invoke에서 라우트 등록:

기존:
```go
fx.Invoke(func(fiberApp *fiber.App, h todohttp.Handler) {
    h.RegisterRoutes(fiberApp)
}),
```

변경:
```go
fx.Invoke(func(api huma.API, h todohttp.Handler) {
    h.RegisterRoutes(api)
}),
```

import 변경:
- `"github.com/gofiber/fiber/v3"` 제거 (더 이상 main.go에서 직접 사용하지 않음)
- `"github.com/danielgtaylor/huma/v2"` 추가

main.go에서 `fiber`를 직접 import하는 부분이 남아있는지 확인한다.
fx.Invoke의 함수 파라미터에서 `*fiber.App`이 사용되지 않으면 import를 제거한다.

**Step 2: 빌드 확인**

```bash
make build
```

Expected: 성공

**Step 3: 개발 서버 실행하여 검증**

```bash
make dev
```

다른 터미널에서:
```bash
# Scalar UI 확인
curl -s http://localhost:42001/docs | head -20

# OpenAPI 스펙 확인
curl -s http://localhost:42001/openapi.json | head -20

# 기존 API 동작 확인
curl -s http://localhost:42001/todos | head -20

# 헬스체크 확인
curl -s http://localhost:42001/livez
```

**Step 4: 커밋**

```bash
git add cmd/server/main.go
git commit -m "main.go에서 huma.API를 통한 라우트 등록으로 변경"
```

---

### Task 7: E2E 테스트 인프라 업데이트

**Files:**
- Modify: `internal/testutil/e2e.go`

**배경:**
huma API 인스턴스가 DI에 등록되어야 핸들러가 작동한다.
testutil의 NewTestApp()은 AppModule()을 사용하므로 이미 huma.API가 포함되어 있다.
다만 반환값에 huma.API가 필요할 수 있으므로 확인한다.

**Step 1: testutil/e2e.go 확인 및 업데이트**

AppModule()에 이미 `fx.Provide(newHumaAPI)`가 포함되어 있으므로,
테스트에서 huma.API가 자동으로 생성된다.

현재 핸들러의 RegisterRoutes가 huma.API를 받으므로, E2E 테스트의 fx.Invoke도 변경이 필요하다.

handler_e2e_test.go의 SetupSuite에서:

기존:
```go
fx.Invoke(func(fiberApp *fiber.App, h todohttp.Handler) {
    h.RegisterRoutes(fiberApp)
}),
```

변경:
```go
fx.Invoke(func(api huma.API, h todohttp.Handler) {
    h.RegisterRoutes(api)
}),
```

import 변경:
- `"github.com/danielgtaylor/huma/v2"` 추가
- `"github.com/gofiber/fiber/v3"` import가 스위트의 app 필드에서 사용되므로 유지

**Step 2: 빌드 확인**

```bash
make build
```

---

### Task 8: E2E 테스트 업데이트

**Files:**
- Modify: `internal/domain/todo/handler/http/handler_e2e_test.go`

**변경 필요 사항:**

1. **import 변경**: `huma.API` 추가
2. **SetupSuite**: huma.API 기반 라우트 등록으로 변경
3. **URL 경로 변경**: huma는 StrictRouting의 trailing slash 문제 없음
   - `/todos/` → `/todos` (POST, GET 컬렉션)
   - `/tags/` → `/tags` (POST, GET 컬렉션)
   - `/todos/?page=1&limit=2` → `/todos?page=1&limit=2`
   - `/todos/:id`, `/tags/:id` 등 개별 리소스 경로는 변경 없음
4. **에러 응답 형식 변경**: huma는 RFC 9457 Problem Details 형식 사용
   - 기존: `{ "code": "NOT_FOUND", "message": "..." }`
   - huma: `{ "title": "Not Found", "status": 404, "detail": "..." }`
   - 에러 상태 코드 검증은 유지, 바디 형식 검증은 제거 또는 huma 형식으로 변경
5. **검증 에러**: huma는 422 대신 자체 유효성 검사 에러 상태 코드 사용
   - huma 기본: 유효성 검사 실패 시 422 Unprocessable Entity 반환 (동일)

**Step 1: E2E 테스트 파일 수정**

핵심 변경:
- 모든 `/todos/`(trailing slash) → `/todos`
- 모든 `/tags/`(trailing slash) → `/tags`
- SetupSuite의 fx.Invoke 변경
- import에 huma 추가

**Step 2: E2E 테스트 실행**

```bash
make e2e
```

Expected: 모든 테스트 통과

**Step 3: 실패하는 테스트 디버깅 및 수정**

huma의 에러 응답 형식이 다를 수 있으므로:
- 상태 코드가 다르면 huma의 에러 매핑 확인
- 응답 바디 형식이 다르면 테스트 단언을 huma 형식에 맞게 수정

**Step 4: 커밋**

```bash
git add internal/domain/todo/handler/http/handler_e2e_test.go
git commit -m "E2E 테스트를 huma 패턴에 맞게 업데이트"
```

---

### Task 9: health E2E 테스트 확인

**Files:**
- Check: `internal/app/health_e2e_test.go`

**배경:**
헬스체크 라우트(/livez, /readyz)는 huma를 사용하지 않고 Fiber 직접 라우트로 유지된다.
huma 도입이 기존 Fiber 라우트에 영향을 주지 않는지 확인한다.

**Step 1: health E2E 테스트 실행**

```bash
cd /Users/me/Desktop/rest-api && go test -tags=e2e ./internal/app/ -run TestHealth -v
```

Expected: 모든 테스트 통과 (변경 없이)

---

### Task 10: 아키텍처 검증 + 전체 테스트

**Files:** 없음 (검증만)

**Step 1: 아키텍처 규칙 검증**

```bash
make arch
```

Expected: 통과. huma import는 프레임워크 의존성이므로 아키텍처 규칙에 위배되지 않는다.

**Step 2: 린트 검증**

```bash
make lint
```

Expected: 통과. 새 코드에 린트 경고가 없어야 한다.

**Step 3: 포맷 검증**

```bash
make fmt
```

**Step 4: 전체 빌드**

```bash
make build
```

**Step 5: 전체 E2E 테스트**

```bash
make e2e
```

Expected: 모든 테스트 통과

**Step 6: 개발 서버 실행 및 수동 검증**

```bash
make dev
```

확인 사항:
- `http://localhost:42001/docs` → Scalar UI 표시
- `http://localhost:42001/openapi.json` → OpenAPI 3.1 스펙 JSON
- `http://localhost:42001/openapi.yaml` → OpenAPI 3.1 스펙 YAML
- `http://localhost:42001/todos` → 빈 Todo 목록
- `http://localhost:42001/livez` → 200 OK
- `http://localhost:42001/readyz` → 200 OK

**Step 7: 최종 커밋**

```bash
git add -A
git commit -m "huma + OpenAPI 3.1 + Scalar UI 통합 완료"
```

---

## 주의사항 체크리스트

- [ ] Fiber v3 어댑터: `c.Context()` → Go context, `c.RequestCtx()` → fasthttp context
- [ ] 경로 파라미터: huma `{id}` → 어댑터가 Fiber `:id`로 변환
- [ ] Trailing slash: huma 라우트는 trailing slash 없이 등록됨
- [ ] AppError: `GetStatus()` 메서드 추가로 huma 호환
- [ ] 에러 응답 형식: huma의 RFC 9457 Problem Details로 변경됨
- [ ] 검증: huma 내장 검증 사용 (go-playground/validator 대신)
- [ ] 헬스체크: Fiber 직접 라우트 유지 (huma 미사용)
- [ ] DI: huma.API가 AppModule의 fx.Provide로 등록됨
