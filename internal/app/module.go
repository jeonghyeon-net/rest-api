// Package app은 애플리케이션의 핵심 인프라를 제공한다.
//
// 이 패키지는 cmd/server/main.go에서 분리된 로거, 서버, 에러 처리, 검증 로직을
// 하나의 재사용 가능한 패키지로 통합한다.
//
// 왜 분리했는가?
// Go에서 package main은 다른 패키지에서 import할 수 없다.
// cmd/server/에 있던 AppError 등의 타입과 DI 설정을
// internal/app/으로 이동하면, 테스트 유틸리티(internal/testutil)에서도
// 동일한 DI 그래프를 재사용할 수 있다.
//
// 설정(Config)은 internal/config 패키지에 별도로 분리되어 있다.
// 이렇게 분리하면 internal/db 등 하위 패키지에서도 Config를 직접 import할 수 있어
// 래퍼 함수 없이 fx가 자동으로 의존성을 주입한다.
//
// NestJS에서 AppModule을 정의하고, main.ts와 테스트에서 모두 사용하는 패턴과 같다:
//
//	// app.module.ts
//	@Module({ imports: [...], providers: [...] })
//	export class AppModule {}
//
//	// main.ts
//	const app = await NestFactory.create(AppModule);
//
//	// test
//	const module = await Test.createTestingModule({ imports: [AppModule] }).compile();
package app

import (
	"go.uber.org/fx"

	"rest-api/internal/db"
)

// AppModule은 프로덕션 DI 의존성을 하나의 fx.Option으로 묶는다.
// main.go와 테스트(testutil)가 이 함수를 공유하여 동일한 DI 그래프를 사용한다.
//
// NestJS의 @Module() 데코레이터와 같은 역할이다:
//
//	@Module({
//	  providers: [LoggerService, FiberApp, DatabaseService],
//	})
//	export class AppModule {}
//
// fx.Options()는 여러 fx.Option을 하나로 합친다.
// NestJS의 imports 배열에 여러 모듈을 나열하는 것과 같다.
//
// fx.Provide()는 생성자 함수를 DI 컨테이너에 등록한다.
// fx가 반환 타입을 분석하여, 해당 타입이 필요한 곳에 자동으로 주입한다.
// NestJS의 providers 배열에 서비스를 등록하는 것과 같다.
//
// fx.Invoke()는 앱 시작 시 자동으로 실행되는 함수를 등록한다.
// NestJS의 onModuleInit()과 유사하다.
func AppModule() fx.Option {
	return fx.Options(
		// newLogger를 DI 컨테이너에 등록한다.
		// *zap.Logger 타입이 필요한 곳에 자동으로 주입된다.
		fx.Provide(newLogger),

		// newFiberApp 함수를 DI 컨테이너에 등록한다.
		// fx는 이 함수의 반환 타입(*fiber.App)을 보고,
		// 다른 곳에서 *fiber.App을 요청하면 이 함수를 호출해서 주입한다.
		fx.Provide(newFiberApp),

		// db.NewDB를 DI 컨테이너에 직접 등록한다.
		// *sql.DB 타입이 필요한 곳에 자동으로 주입된다.
		// NewDB 내부에서 SQLite 연결 생성 -> PRAGMA 설정까지 처리한다.
		//
		// db.NewDB는 *config.Config를 매개변수로 받으므로,
		// fx가 DI 컨테이너에서 *config.Config를 찾아 자동으로 주입한다.
		// 이전에는 string 타입의 dbPath를 전달하기 위해 래퍼 함수가 필요했지만,
		// Config를 internal/config 패키지로 분리한 덕분에
		// db 패키지가 config를 직접 import할 수 있어 래퍼가 불필요해졌다.
		//
		// NestJS에서 providers에 서비스를 직접 등록하는 것과 같다:
		//   providers: [DatabaseService]  // ConfigService는 DI가 자동 주입
		fx.Provide(db.NewDB),

		// 마이그레이션을 별도 단계로 실행한다.
		// NewDB와 분리하여 fx.Invoke로 등록하면, *sql.DB가 주입된 후 자동 실행된다.
		// 테스트에서 fx.Replace로 DB를 교체해도 교체된 DB에 마이그레이션이 실행된다.
		fx.Invoke(db.RunMigrations),
	)
}
