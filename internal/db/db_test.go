package db

// ─────────────────────────────────────────────────────────────────────────────
// db_test.go — openDB 함수의 단위 테스트
// ─────────────────────────────────────────────────────────────────────────────
//
// Go의 테스트 파일은 반드시 _test.go 접미사를 가져야 한다.
// go test 명령어가 이 접미사를 보고 테스트 파일을 자동으로 인식한다.
// NestJS에서 .spec.ts 파일이 Jest에 의해 자동 인식되는 것과 같다.
//
// 같은 패키지(package db)에 테스트를 작성하면, 비공개(unexported) 함수도 테스트할 수 있다.
// 이것을 "화이트박스 테스트"라고 한다.
// NestJS에서 private 메서드를 테스트하려면 우회가 필요하지만,
// Go에서는 같은 패키지 내 테스트 파일에서 소문자 함수에 바로 접근 가능하다.
// ─────────────────────────────────────────────────────────────────────────────

import (
	"path/filepath"
	"testing"

	"go.uber.org/goleak"
)

// TestMain은 이 패키지의 모든 테스트를 감싸는 진입점이다.
//
// Go의 testing 패키지에서 TestMain이라는 이름은 특별한 의미를 가진다:
// 이 함수가 정의되면, go test가 Test* 함수를 직접 실행하지 않고
// TestMain을 먼저 호출한다. m.Run()으로 실제 테스트를 실행하고,
// os.Exit()으로 결과를 반환한다.
//
// NestJS에서 Jest의 globalSetup/globalTeardown과 유사한 역할이다.
//
// goleak.VerifyTestMain(m)은 Uber의 goroutine 누수 검출기다.
// 모든 테스트가 끝난 후 아직 실행 중인 goroutine이 있으면 테스트를 실패시킨다.
// goroutine은 Go의 경량 스레드로, NestJS에서 Promise가 resolve되지 않고
// 메모리에 남아있는 것과 비슷한 문제를 감지한다.
//
// 왜 필요한가?
// DB 연결, 타이머, 채널 등을 사용하는 코드에서 goroutine이 정리되지 않으면
// 메모리 누수와 예측 불가능한 동작이 발생한다.
// goleak은 이런 누수를 테스트 시점에 잡아준다.
func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}

// TestOpenDB는 openDB 함수가 SQLite 데이터베이스를 올바르게 여는지 검증한다.
//
// Go의 테스트 함수는 반드시 다음 규칙을 따른다:
//   - 함수 이름이 Test로 시작해야 한다 (대문자 T)
//   - *testing.T를 매개변수로 받아야 한다
//
// NestJS에서 describe('openDB', () => { it('should ...') })로 작성하는 것과 유사하다.
// 다만 Go에서는 별도의 describe/it 래퍼 없이 함수 이름으로 구분한다.
//
// *testing.T는 테스트 컨텍스트 객체로, 다음과 같은 메서드를 제공한다:
//   - t.Fatalf(): 에러 메시지를 출력하고 즉시 테스트를 중단한다 (throw new Error와 유사)
//   - t.Errorf(): 에러를 기록하지만 테스트를 계속 진행한다 (console.error와 유사)
//   - t.TempDir(): 테스트 전용 임시 디렉터리를 생성하고, 테스트 종료 시 자동 삭제한다
func TestOpenDB(t *testing.T) {
	// t.TempDir()은 각 테스트마다 고유한 임시 디렉터리를 생성한다.
	// 테스트가 끝나면 Go가 자동으로 이 디렉터리를 삭제한다.
	// NestJS에서 beforeEach/afterEach로 임시 파일을 정리하는 것을 Go가 자동 처리한다.
	path := filepath.Join(t.TempDir(), "test.db")

	// openDB는 소문자로 시작하므로 unexported(비공개) 함수다.
	// 같은 패키지(package db)에 있는 테스트 파일이므로 접근 가능하다.
	db, err := openDB(path)
	if err != nil {
		t.Fatalf("openDB 실패: %v", err)
	}

	// defer는 함수가 종료될 때(정상/에러 모두) 실행할 코드를 등록한다.
	// NestJS에서 afterEach(() => db.close())와 비슷하지만,
	// Go의 defer는 함수 스코프에서 LIFO(후입선출) 순서로 실행된다.
	//
	// 여기서는 테스트 종료 시 DB 연결을 닫아 리소스 누수를 방지한다.
	defer func() {
		if err := db.Close(); err != nil {
			t.Errorf("DB Close 실패: %v", err)
		}
	}()

	// Ping()은 DB 연결이 실제로 작동하는지 확인한다.
	// 네트워크 DB에서는 실제 네트워크 요청을 보내지만,
	// SQLite에서는 파일이 정상적으로 접근 가능한지 확인한다.
	if err := db.Ping(); err != nil {
		t.Fatalf("DB Ping 실패: %v", err)
	}

	// ─── PRAGMA 설정 검증 (테이블 주도 테스트) ──────────────────────────
	//
	// 테이블 주도 테스트(table-driven test)는 Go에서 가장 널리 쓰이는 테스트 패턴이다.
	// 테스트 케이스를 구조체 슬라이스로 정의하고, 반복문으로 각 케이스를 실행한다.
	//
	// NestJS/Jest의 it.each() 또는 describe.each()와 같은 개념이다:
	//
	//   it.each([
	//     ['journal_mode', 'wal'],
	//     ['foreign_keys', '1'],
	//   ])('PRAGMA %s should be %s', (pragma, expected) => { ... });
	//
	// 장점:
	//   - 새 PRAGMA 검증을 추가할 때 구조체만 추가하면 된다 (로직 변경 불필요)
	//   - 각 케이스가 t.Run()의 서브테스트로 실행되어 개별 실패 추적이 가능하다
	//   - 테스트 실패 시 어떤 PRAGMA가 실패했는지 이름으로 바로 확인할 수 있다
	pragmaTests := []struct {
		name  string // 서브테스트 이름 겸 PRAGMA 이름 — go test -run TestOpenDB/journal_mode 형태로 개별 실행 가능
		query string // 실행할 PRAGMA 쿼리
		want  string // 기대하는 결과값
	}{
		// WAL(Write-Ahead Logging) 모드는 읽기와 쓰기를 동시에 허용하는 모드로,
		// 기본 DELETE 모드보다 동시성이 훨씬 좋다.
		{name: "journal_mode", query: "PRAGMA journal_mode", want: "wal"},
		// SQLite는 기본적으로 외래 키 제약을 무시한다.
		// foreign_keys=ON으로 명시적으로 활성화해야 한다.
		// 결과값 "1"은 활성화 상태를 의미한다 (0=비활성, 1=활성).
		{name: "foreign_keys", query: "PRAGMA foreign_keys", want: "1"},
		// busy_timeout은 잠금 충돌 시 즉시 실패하지 않고 대기하는 시간(ms)이다.
		// 5000ms 동안 기다려도 잠금이 풀리지 않으면 "database is locked" 에러를 반환한다.
		{name: "busy_timeout", query: "PRAGMA busy_timeout", want: "5000"},
	}

	for _, tt := range pragmaTests {
		// t.Run()은 서브테스트를 생성한다.
		// NestJS의 it() 블록 하나와 같다.
		// 실행 시 "TestOpenDB/journal_mode" 형태로 출력되어 어떤 케이스가 실패했는지 알 수 있다.
		// go test -run TestOpenDB/journal_mode 로 특정 케이스만 실행할 수도 있다.
		t.Run(tt.name, func(t *testing.T) {
			var got string
			// QueryRow()는 단일 행을 조회한다. NestJS에서 db.query(...).then(rows => rows[0])과 유사.
			// Scan()은 조회 결과를 Go 변수에 바인딩한다. 포인터(&)를 전달해야 값이 채워진다.
			if err := db.QueryRow(tt.query).Scan(&got); err != nil {
				t.Fatalf("%s 조회 실패: %v", tt.name, err)
			}
			if got != tt.want {
				t.Errorf("%s: got %q, want %q", tt.name, got, tt.want)
			}
		})
	}
}

// TestOpenDB_CreatesDirectory는 DB 파일의 상위 디렉터리가 없어도
// openDB가 자동으로 생성하는지 검증한다.
//
// 예를 들어 DB_PATH가 "./data/nested/app.db"인데 data/nested/ 디렉터리가 없는 경우,
// openDB가 os.MkdirAll로 필요한 디렉터리를 모두 생성해야 한다.
// NestJS에서 fs.mkdirSync(dir, { recursive: true })와 같다.
func TestOpenDB_CreatesDirectory(t *testing.T) {
	// 존재하지 않는 중첩 디렉터리 경로를 의도적으로 지정한다.
	// "nested/deep/" 디렉터리는 아직 없으므로, openDB가 자동 생성해야 한다.
	path := filepath.Join(t.TempDir(), "nested", "deep", "test.db")

	db, err := openDB(path)
	if err != nil {
		t.Fatalf("openDB 실패 (중첩 디렉터리): %v", err)
	}
	// _ = db.Close()는 에러를 명시적으로 무시하는 Go 관용 패턴이다.
	// 테스트 정리(cleanup) 코드에서는 Close 에러가 크게 중요하지 않으므로 무시한다.
	defer func() { _ = db.Close() }()

	if err := db.Ping(); err != nil {
		t.Fatalf("DB Ping 실패: %v", err)
	}
}
