package main

import "go.uber.org/zap"

// newLogger는 환경에 따라 적절한 zap 로거를 생성한다.
// fx.Provide()에 의해 DI 컨테이너에 등록되며, *zap.Logger 타입이 필요한 곳에 자동 주입된다.
//
// zap은 우버가 만든 구조화된(structured) 로깅 라이브러리다.
// fmt.Printf와 달리 JSON 형태로 로그를 출력하여, DataDog, ELK 등 로그 수집 시스템에서
// 필드별 검색/필터링이 가능하다.
//
// NestJS의 내장 Logger가 기본적으로 구조화된 출력을 해주는데,
// Go에는 그런 기본 제공이 없어서 zap을 사용한다.
// 성능도 fmt.Printf보다 빠르다 (리플렉션 없이 타입별 전용 메서드 사용).
//
// 환경별 동작:
//   - development: 사람이 읽기 쉬운 컬러 로그 (zap.NewDevelopment)
//   - production:  JSON 구조화 로그 (zap.NewProduction)
func newLogger(cfg *Config) (*zap.Logger, error) {
	if cfg.AppEnv == "production" {
		// 프로덕션: JSON 형태의 구조화 로그
		// 예: {"level":"error","ts":1709369400,"msg":"서버 에러","port":"42001"}
		return zap.NewProduction()
	}

	// 개발 환경: 사람이 읽기 쉬운 형태
	// 예: 2024-03-02T18:30:00.000+0900  ERROR  서버 에러  {"port": "42001"}
	return zap.NewDevelopment()
}
