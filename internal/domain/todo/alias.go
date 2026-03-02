package todo

// 이 파일은 todo 도메인의 외부 공개 API를 정의한다.
// 다른 도메인이나 handler에서 todo 도메인의 타입을 사용할 때
// 이 파일의 타입 별칭(alias)을 통해서만 접근한다.
//
// 아키텍처 규칙: 외부 패키지는 도메인의 내부 패키지(subdomain/*)를
// 직접 import할 수 없다. alias.go가 유일한 진입점이다.
//
// Go의 타입 별칭(=)은 원본 타입과 완전히 동일한 타입을 만든다.
// type A = B 로 선언하면 A와 B는 서로 교환 가능하며(interchangeable),
// 타입 변환(conversion) 없이 그대로 사용할 수 있다.
// 이는 일반적인 타입 정의(type A B)와 다르다 — 타입 정의는 새로운 타입을 만든다.
//
// NestJS에서 모듈의 exports 배열에 서비스를 등록하여
// 다른 모듈에서 사용할 수 있게 하는 것과 같다.
// 예: @Module({ exports: [TodoService] })

import (
	"database/sql"

	"rest-api/internal/domain/todo/svc"
)

// Service는 Todo 도메인의 Public Service 인터페이스다.
// handler와 saga에서 이 타입을 통해 Todo 도메인에 접근한다.
//
// 사용 예:
//
//	func NewHandler(svc todo.Service) { ... }
type Service = svc.Service

// NewService는 Todo 도메인의 Public Service 생성자다.
// fx.Provide에 등록하여 DI 컨테이너가 자동으로 주입하게 한다.
//
// var 대신 함수로 감싸는 이유:
// gochecknoglobals 린터가 패키지 수준의 변수(var) 사용을 금지하기 때문이다.
// 함수로 감싸도 fx.Provide(todo.NewService)로 동일하게 사용 가능하다.
//
// 사용 예:
//
//	fx.Provide(todo.NewService)
func NewService(db *sql.DB) Service {
	return svc.New(db)
}
