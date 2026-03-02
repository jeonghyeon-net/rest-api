package main

import "github.com/go-playground/validator/v10"

// structValidator는 Fiber의 StructValidator 인터페이스를 구현하는 어댑터다.
// Fiber가 c.Bind().Body() 등으로 요청을 파싱할 때 자동으로 검증을 수행하게 해준다.
//
// NestJS에서 app.useGlobalPipes(new ValidationPipe())으로
// 전역 검증 파이프를 설정하는 것과 같은 역할이다.
// NestJS의 ValidationPipe가 class-validator 데코레이터를 자동 실행하는 것처럼,
// 이 어댑터가 Go 구조체의 validate 태그를 자동 실행한다.
//
// 사용 예시:
//
//	type CreateUserRequest struct {
//	    Name  string `json:"name"  validate:"required,min=2"`
//	    Email string `json:"email" validate:"required,email"`
//	}
//
//	func handler(c fiber.Ctx) error {
//	    req := new(CreateUserRequest)
//	    if err := c.Bind().Body(req); err != nil {
//	        return err  // 파싱 실패 또는 검증 실패 시 에러 반환
//	    }
//	    // 여기 도달하면 이미 검증 완료된 데이터
//	}
//
// 내부 동작:
//  1. Fiber가 JSON → 구조체로 파싱 (sonic 사용)
//  2. 파싱 성공 시 structValidator.Validate() 자동 호출
//  3. validate 태그 규칙에 따라 필드별 검증
//  4. 실패 시 validator.ValidationErrors 반환
//
// 성능:
// go-playground/validator는 구조체 타입 정보를 최초 1회만 파싱하고 캐시한다.
// 이후 요청에서는 캐시된 규칙으로 값만 비교하므로 필드당 ~28ns로 매우 빠르다.
type structValidator struct {
	// validate는 go-playground/validator의 검증 엔진 인스턴스다.
	// NestJS의 class-validator가 내부적으로 메타데이터 저장소를 갖는 것처럼,
	// 이 인스턴스가 검증 규칙 캐시와 커스텀 검증 함수를 관리한다.
	validate *validator.Validate
}

// Validate는 Fiber의 StructValidator 인터페이스가 요구하는 메서드다.
// Fiber가 Bind() 과정에서 구조체 파싱 후 자동으로 호출한다.
//
// any 타입(= interface{})을 받아서 validate.Struct()로 검증한다.
// Go에서 any는 TypeScript의 unknown과 비슷한 개념으로,
// 어떤 타입이든 받을 수 있는 빈 인터페이스다.
func (v *structValidator) Validate(out any) error {
	return v.validate.Struct(out)
}

// newStructValidator는 go-playground/validator 인스턴스를 생성하여
// Fiber의 StructValidator 인터페이스를 구현하는 어댑터를 반환한다.
//
// 이 함수는 newFiberApp에서 fiber.Config.StructValidator에 설정된다.
func newStructValidator() *structValidator {
	return &structValidator{
		validate: validator.New(),
	}
}
