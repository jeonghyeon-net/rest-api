package app

import (
	"errors"

	"github.com/go-playground/validator/v10"
	"github.com/gofiber/fiber/v3"
	"go.uber.org/zap"
)

// AppError는 애플리케이션 전체에서 사용하는 구조화된 에러 타입이다.
// NestJS의 HttpException과 같은 역할을 한다.
//
// Go에서 에러는 단순히 error 인터페이스(Error() string)를 구현하면 되지만,
// HTTP API에서는 상태 코드와 에러 코드가 필요하다.
// AppError는 이 정보를 하나의 타입으로 통합한다.
//
// NestJS에서는 이렇게 사용한다:
//
//	throw new HttpException('사용자를 찾을 수 없습니다', HttpStatus.NOT_FOUND)
//
// Go에서는 이렇게 사용한다:
//
//	return ErrNotFound
//	return NewAppError(http.StatusBadRequest, "INVALID_INPUT", "잘못된 입력입니다")
type AppError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Status  int    `json:"-"`
}

// Error는 Go의 error 인터페이스를 구현한다.
// 이 메서드 하나만 구현하면 AppError를 일반 error처럼 사용할 수 있다.
// (Go에서는 인터페이스를 명시적으로 implements 하지 않고,
// 메서드만 구현하면 자동으로 인터페이스를 충족한다 — "덕 타이핑")
func (e *AppError) Error() string {
	return e.Message
}

// NewAppError는 새로운 AppError를 생성한다.
// 핸들러에서 특정 에러를 반환할 때 사용한다.
//
// 사용 예시:
//
//	return NewAppError(http.StatusBadRequest, "INVALID_EMAIL", "이메일 형식이 올바르지 않습니다")
func NewAppError(status int, code, message string) *AppError {
	return &AppError{
		Status:  status,
		Code:    code,
		Message: message,
	}
}

// 자주 사용되는 에러를 미리 정의한다.
// 우버 스타일 가이드에 따라 에러 변수는 Err 접두사를 사용한다.
//
// NestJS에서 NotFoundException, UnauthorizedException 등을
// 미리 만들어두는 것과 같은 패턴이다.
//
// 사용 예시:
//
//	if user == nil {
//	    return ErrNotFound
//	}
var (
	ErrNotFound     = NewAppError(fiber.StatusNotFound, "NOT_FOUND", "리소스를 찾을 수 없습니다")
	ErrUnauthorized = NewAppError(fiber.StatusUnauthorized, "UNAUTHORIZED", "인증이 필요합니다")
	ErrForbidden    = NewAppError(fiber.StatusForbidden, "FORBIDDEN", "접근 권한이 없습니다")
	ErrBadRequest   = NewAppError(fiber.StatusBadRequest, "BAD_REQUEST", "잘못된 요청입니다")
	ErrInternal     = NewAppError(fiber.StatusInternalServerError, "INTERNAL_ERROR", "서버 내부 오류가 발생했습니다")
)

// newErrorHandler는 Fiber의 커스텀 에러 핸들러를 생성한다.
// NestJS의 전역 ExceptionFilter와 같은 역할이다.
//
// Fiber에서 핸들러가 error를 반환하면 이 함수가 호출된다.
// 에러 타입에 따라 적절한 JSON 응답을 생성한다:
//   - *AppError:                  정의된 상태 코드와 에러 코드로 응답
//   - *fiber.Error:               Fiber 내장 에러 (예: 404 라우트 미매칭)
//   - validator.ValidationErrors: 검증 실패 시 필드별 상세 에러
//   - 그 외:                      500 Internal Server Error
//
// NestJS에서는 @Catch() 데코레이터로 ExceptionFilter를 만드는데,
// Go에서는 함수를 반환하는 함수(클로저) 패턴으로 구현한다.
// logger를 클로저로 캡처하여 에러 발생 시 구조화된 로그를 남길 수 있다.
func newErrorHandler(logger *zap.Logger) func(fiber.Ctx, error) error {
	return func(c fiber.Ctx, err error) error {
		// errors.AsType은 에러 체인에서 특정 타입의 에러를 찾는 Go 1.23+ 제네릭 함수다.
		// 기존 errors.As와 같은 역할이지만, 변수를 미리 선언할 필요 없이
		// 타입 파라미터로 바로 추출할 수 있어 코드가 간결하다.
		// NestJS에서 instanceof로 에러 타입을 확인하는 것과 비슷하다.
		//
		// Go에서는 에러를 fmt.Errorf("...: %w", err)로 감싸는(wrap) 패턴이 일반적인데,
		// errors.AsType은 감싸진 에러 안에서도 원래 타입을 찾아낸다.
		// (TypeScript에서는 에러 체인 개념이 없어서 직접 대응되는 기능이 없다)

		// 1) AppError: 비즈니스 로직에서 의도적으로 반환한 에러
		if appErr, ok := errors.AsType[*AppError](err); ok {
			return c.Status(appErr.Status).JSON(appErr)
		}

		// 2) Fiber 내장 에러: 라우트 미매칭(404), 바디 파싱 실패 등
		if fiberErr, ok := errors.AsType[*fiber.Error](err); ok {
			return c.Status(fiberErr.Code).JSON(&AppError{
				Code:    "FIBER_ERROR",
				Message: fiberErr.Message,
			})
		}

		// 3) 검증 에러: go-playground/validator가 반환하는 필드별 검증 실패
		//    NestJS의 ValidationPipe가 class-validator 에러를 변환하는 것과 유사하다.
		if validationErrors, ok := errors.AsType[validator.ValidationErrors](err); ok {
			// 각 필드의 검증 실패 정보를 배열로 변환한다.
			details := make([]map[string]string, 0, len(validationErrors))
			for _, fe := range validationErrors {
				details = append(details, map[string]string{
					"field":   fe.Field(),
					"tag":     fe.Tag(),
					"message": fe.Error(),
				})
			}
			return c.Status(fiber.StatusUnprocessableEntity).JSON(fiber.Map{
				"code":    "VALIDATION_FAILED",
				"message": "요청 데이터 검증에 실패했습니다",
				"details": details,
			})
		}

		// 4) 예상치 못한 에러: 500으로 응답하고 상세 로그를 남긴다.
		//    클라이언트에게는 내부 구현을 노출하지 않는다 (보안).
		logger.Error("처리되지 않은 에러",
			zap.Error(err),
			zap.String("path", c.Path()),
			zap.String("method", c.Method()),
		)
		return c.Status(fiber.StatusInternalServerError).JSON(ErrInternal)
	}
}
