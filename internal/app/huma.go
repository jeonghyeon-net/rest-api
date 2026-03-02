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
	"fmt"
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
// huma.Context 인터페이스가 url.URL을 반환하므로 에러를 전파할 수 없다.
// 파싱 실패 시 빈 URL을 반환한다 (실제로 유효한 HTTP 요청에서는 발생하지 않음).
func (c *fiberCtx) URL() url.URL {
	u, err := url.Parse(string(c.orig.Request().RequestURI()))
	if err != nil {
		return url.URL{}
	}
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
// fasthttp의 Header.All()은 Go 1.23+ 이터레이터(iter.Seq2)를 반환한다.
// range-for로 순회하면서 키와 값([]byte)을 string으로 변환하여 콜백에 전달한다.
func (c *fiberCtx) EachHeader(cb func(name, value string)) {
	for key, value := range c.orig.Request().Header.All() {
		cb(string(key), string(value))
	}
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
// net.Conn의 SetReadDeadline 에러를 래핑하여 반환한다.
func (c *fiberCtx) SetReadDeadline(deadline time.Time) error {
	if err := c.orig.RequestCtx().Conn().SetReadDeadline(deadline); err != nil {
		return fmt.Errorf("read deadline 설정 실패: %w", err)
	}
	return nil
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
//
// Fiber v3에서는 핸들러 타입이 any로 선언되어 있으며,
// 내부적으로 fiber.Handler(func(Ctx) error)로 변환된다.
type fiberRouter interface {
	Add(methods []string, path string, handler any, handlers ...any) fiber.Router
}

// fiberTester는 Fiber v3 앱의 Test 메서드를 추상화한 인터페이스다.
// ServeHTTP에서 *http.Request를 Fiber로 전달할 때 사용한다.
type fiberTester interface {
	Test(req *http.Request, config ...fiber.TestConfig) (*http.Response, error)
}

// fiberAdapter는 huma.Adapter를 구현하는 Fiber v3 어댑터다.
type fiberAdapter struct {
	router fiberRouter // 라우트 등록용 (앱 또는 그룹)
	tester fiberTester // HTTP 테스트용 (항상 앱)
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
	// Fiber v3에서 Add의 첫 번째 인자는 메서드 슬라이스이고,
	// 핸들러 파라미터 타입은 any이지만 내부에서 fiber.Handler로 변환된다.
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
	//nolint:errcheck // 이미 WriteHeader를 호출했으므로 응답 바디 쓰기 에러를 처리할 수 없다
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
