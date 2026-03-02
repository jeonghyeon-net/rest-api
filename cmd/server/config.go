package main

import (
	"os"
	"time"
)

// Config는 애플리케이션의 모든 설정을 하나의 구조체로 모은 것이다.
// NestJS의 ConfigService와 같은 역할을 한다.
//
// 왜 구조체로 모으는가?
// 설정이 getEnv("PORT", "42001") 같은 인라인 호출로 여기저기 흩어져 있으면,
// 어떤 환경변수가 쓰이는지 파악하기 어렵고 테스트에서 값을 교체하기도 힘들다.
// 구조체로 묶으면:
//  1. 설정 목록을 한눈에 볼 수 있다
//  2. fx를 통해 어디서든 *Config를 주입받아 사용할 수 있다
//  3. 테스트에서 Config를 직접 생성하여 주입할 수 있다
//
// NestJS에서 ConfigModule.forRoot()로 설정을 중앙화하고
// configService.get('PORT')로 접근하는 것과 같은 패턴이다.
type Config struct {
	// AppEnv는 실행 환경을 나타낸다. "development" 또는 "production".
	// 로거 형식, 디버그 모드 등 환경별 동작을 결정한다.
	AppEnv string

	// Port는 HTTP 서버가 바인딩할 포트 번호다.
	Port string

	// ReadTimeout은 클라이언트가 요청을 보내는 데 허용되는 최대 시간이다.
	// Slowloris 공격 방어에 효과적이다.
	ReadTimeout time.Duration

	// WriteTimeout은 서버가 응답을 보내는 데 허용되는 최대 시간이다.
	WriteTimeout time.Duration

	// IdleTimeout은 Keep-Alive 연결의 최대 유휴 시간이다.
	IdleTimeout time.Duration

	// BodyLimit은 요청 바디의 최대 크기(바이트)다.
	BodyLimit int

	// ShutdownTimeout은 graceful shutdown 시 대기하는 최대 시간이다.
	ShutdownTimeout time.Duration
}

// newConfig는 환경변수에서 설정값을 읽어 Config 구조체를 생성한다.
// fx.Supply()에 의해 DI 컨테이너에 등록되며, *Config 타입이 필요한 곳에 자동 주입된다.
//
// 모든 설정은 이 함수에서 한 번에 읽힌다.
// 환경변수가 없으면 안전한 기본값이 사용된다.
func newConfig() *Config {
	return &Config{
		AppEnv:          getEnv("APP_ENV", "development"),
		Port:            getEnv("PORT", "42001"),
		ReadTimeout:     10 * time.Second,
		WriteTimeout:    10 * time.Second,
		IdleTimeout:     120 * time.Second,
		BodyLimit:       4 * 1024 * 1024, // 4MB
		ShutdownTimeout: 30 * time.Second,
	}
}

// getEnv는 환경변수를 조회하고, 없으면 기본값을 반환하는 헬퍼 함수다.
// NestJS의 configService.get('KEY', 'default')와 같은 역할이다.
//
// os.LookupEnv는 Go 표준 라이브러리 함수로, (값, 존재여부) 두 값을 반환한다.
// Go에서는 이렇게 두 번째 반환값으로 "값이 있는지 없는지"를 알려주는 패턴이 매우 흔하다.
// (map 조회, 타입 단언 등에서도 동일한 패턴 사용)
func getEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}
