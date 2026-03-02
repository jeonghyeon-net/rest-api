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
)

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
		// t.Fatalf는 에러 메시지를 출력하고 이 테스트를 즉시 중단한다.
		// %v는 Go의 포맷 동사(format verb)로, 값을 기본 형식으로 출력한다.
		// NestJS에서 throw new Error(`openDB 실패: ${err}`)와 유사하다.
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

	// ─── WAL 모드 확인 ──────────────────────────────────────────────────
	// PRAGMA journal_mode는 SQLite의 저널링 방식을 반환한다.
	// WAL(Write-Ahead Logging) 모드는 읽기와 쓰기를 동시에 허용하는 모드로,
	// 기본 DELETE 모드보다 동시성이 훨씬 좋다.
	//
	// QueryRow()는 단일 행을 조회한다. NestJS에서 db.query(...).then(rows => rows[0])과 유사.
	// Scan()은 조회 결과를 Go 변수에 바인딩한다. 포인터(&)를 전달해야 값이 채워진다.
	// Go에서 &는 변수의 메모리 주소(포인터)를 얻는 연산자다.
	// NestJS에서는 참조 타입이 기본이지만, Go에서는 명시적으로 포인터를 전달해야 한다.
	// ────────────────────────────────────────────────────────────────────
	var journalMode string
	if err := db.QueryRow("PRAGMA journal_mode").Scan(&journalMode); err != nil {
		t.Fatalf("journal_mode 조회 실패: %v", err)
	}
	if journalMode != "wal" {
		// t.Errorf는 에러를 기록하지만 테스트를 중단하지 않는다.
		// 이 검사가 실패해도 다음 검사(foreign_keys)까지 실행된다.
		// %q는 문자열을 따옴표로 감싸서 출력하는 포맷 동사다. 디버깅 시 유용하다.
		t.Errorf("journal_mode: got %q, want %q", journalMode, "wal")
	}

	// ─── Foreign Keys 활성화 확인 ──────────────────────────────────────
	// SQLite는 기본적으로 외래 키(foreign key) 제약을 무시한다.
	// PRAGMA foreign_keys = ON으로 명시적으로 활성화해야 한다.
	// PostgreSQL/MySQL에서는 기본 활성화지만 SQLite는 아니다.
	// ────────────────────────────────────────────────────────────────────
	var foreignKeys int
	if err := db.QueryRow("PRAGMA foreign_keys").Scan(&foreignKeys); err != nil {
		t.Fatalf("foreign_keys 조회 실패: %v", err)
	}
	if foreignKeys != 1 {
		t.Errorf("foreign_keys: got %d, want 1", foreignKeys)
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
