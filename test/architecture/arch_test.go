// Package architecture_test는 프로젝트의 아키텍처 규칙을 자동 검증하는 테스트다.
//
// "go test ./test/architecture/..." 명령으로 실행하면
// 프로젝트의 모든 Go 소스 파일을 AST(추상 구문 트리)로 파싱해서
// 5가지 카테고리의 아키텍처 규칙을 검증한다:
//
//  1. Dependencies (의존성): import 방향이 올바른지
//  2. Naming (네이밍): 패키지명, 타입명이 규칙을 따르는지
//  3. InterfacePatterns (인터페이스 패턴): 인터페이스+구현체+생성자 패턴
//  4. Structure (구조): 디렉토리 구조가 정해진 형태인지
//  5. Sqlc (코드 생성): sqlc 설정 완전성 + 수기 코드 차단
//
// 핵심 원칙:
//
//	규칙을 CLAUDE.md나 문서에 적는 대신, 코드로 강제한다.
//	테스트가 통과하면 규칙을 지킨 것이고, 실패하면 위반한 것이다.
//	이렇게 하면 사람이든 AI든 규칙을 "잊어서" 위반하는 일이 없어진다.
//
// 사용 방법:
//
//	# 전체 아키텍처 테스트 실행
//	go test ./test/architecture/... -v
//
//	# 특정 규칙만 실행 (예: 의존성 규칙만)
//	go test ./test/architecture/... -v -run TestArchitecture/Dependencies
package architecture_test

import (
	"os"
	"path/filepath"
	"testing"

	"rest-api/test/architecture/analyzer"
	"rest-api/test/architecture/report"
	"rest-api/test/architecture/ruleset"
)

// projectRoot는 프로젝트 루트 디렉토리(go.mod가 있는 디렉토리)의 절대 경로를 반환한다.
//
// 테스트 실행 시 작업 디렉토리(working directory)는 테스트 파일이 있는 곳이다.
// 즉, 이 테스트는 test/architecture/ 에서 실행된다.
// go.mod는 프로젝트 루트에 있으므로, 현재 디렉토리에서 위로 올라가면서 찾는다.
//
// t.Helper()란?
//
//	이 함수가 "테스트 헬퍼"임을 표시한다.
//	테스트 실패 시 에러 메시지에 이 함수 내부 줄번호가 아닌
//	이 함수를 호출한 곳의 줄번호가 표시되어 디버깅이 쉬워진다.
func projectRoot(t *testing.T) string {
	t.Helper()

	// os.Getwd: 현재 작업 디렉토리를 반환 (pwd 명령과 같음)
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}

	// 현재 디렉토리에서 위로 올라가면서 go.mod를 찾는다
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir // go.mod를 찾았으면 여기가 프로젝트 루트
		}
		parent := filepath.Dir(dir) // 한 단계 상위 디렉토리
		if parent == dir {
			// 더 이상 올라갈 수 없으면 (루트 디렉토리에 도달) 실패
			t.Fatal("could not find project root (go.mod)")
		}
		dir = parent
	}
}

// setup은 테스트 실행 전에 필요한 준비 작업을 수행한다.
//
// 반환값:
//   - *ruleset.Config: 프로젝트 설정 (모듈명, 루트 경로)
//   - []*analyzer.FileInfo: 프로젝트의 모든 Go 파일 분석 결과
//
// internal/ 디렉토리가 아직 없는 경우 (프로젝트 초기):
//
//	files는 nil이 되고, 모든 규칙 검사에서 "검사할 파일이 없음" → 위반 없음이 된다.
func setup(t *testing.T) (*ruleset.Config, []*analyzer.FileInfo) {
	t.Helper()

	root := projectRoot(t)

	// 프로젝트 설정 생성 (go.mod에서 모듈명을 읽어옴)
	cfg, err := ruleset.NewConfig(root)
	if err != nil {
		t.Fatalf("failed to create config: %v", err)
	}

	// internal/ 디렉토리 안의 모든 Go 파일을 파싱
	files, err := analyzer.ParseDirectory(filepath.Join(root, "internal"))
	if err != nil {
		t.Fatalf("failed to parse directory: %v", err)
	}

	return cfg, files
}

// TestArchitecture는 5가지 아키텍처 규칙을 모두 실행하고 종합 결과를 보고한다.
//
// Go 테스트 구조 설명:
//   - TestXxx 형태의 함수가 테스트 함수다 (반드시 Test로 시작해야 함)
//   - t.Run("이름", func(t *testing.T) { ... }) 으로 서브테스트를 만들 수 있다
//   - 서브테스트는 독립적으로 실행 가능: go test -run TestArchitecture/Naming
//
// 결과 처리:
//   - ERROR 심각도 위반이 있으면 → 테스트 FAIL (t.Fatal)
//   - WARNING만 있으면 → 테스트 PASS (로그에 경고만 출력)
//   - 위반 없으면 → 테스트 PASS
func TestArchitecture(t *testing.T) {
	cfg, files := setup(t)

	// 모든 서브테스트의 위반을 모아서 마지막에 종합 요약을 출력한다
	var allViolations []report.Violation

	// ── 서브테스트 1: 의존성 규칙 ──
	t.Run("Dependencies", func(t *testing.T) {
		vs := ruleset.CheckDependencies(files, cfg)
		allViolations = append(allViolations, vs...)
		reportViolations(t, vs)
	})

	// ── 서브테스트 2: 네이밍 규칙 ──
	t.Run("Naming", func(t *testing.T) {
		vs := ruleset.CheckNaming(files, cfg)
		allViolations = append(allViolations, vs...)
		reportViolations(t, vs)
	})

	// ── 서브테스트 3: 인터페이스 패턴 규칙 ──
	t.Run("InterfacePatterns", func(t *testing.T) {
		vs := ruleset.CheckInterfacePatterns(files, cfg)
		allViolations = append(allViolations, vs...)
		reportViolations(t, vs)
	})

	// ── 서브테스트 4: 디렉토리 구조 규칙 ──
	t.Run("Structure", func(t *testing.T) {
		vs := ruleset.CheckStructure(cfg)
		allViolations = append(allViolations, vs...)
		reportViolations(t, vs)
	})

	// ── 서브테스트 5: sqlc 규칙 ──
	t.Run("Sqlc", func(t *testing.T) {
		vs := ruleset.CheckSqlc(cfg)
		allViolations = append(allViolations, vs...)
		reportViolations(t, vs)
	})

	// 종합 요약 출력
	t.Log("\n" + report.Summary(allViolations))

	// ERROR 심각도 위반이 하나라도 있으면 테스트를 실패시킨다
	if report.HasErrors(allViolations) {
		t.Fatal("architecture violations with ERROR severity found")
	}
}

// reportViolations는 위반 목록을 테스트 로그에 출력한다.
//
// t.Error: ERROR 심각도 위반을 출력하고 테스트를 "실패"로 표시한다 (실행은 계속됨).
// t.Log: WARNING 심각도 위반을 출력만 하고 테스트 결과에는 영향 없다.
//
// t.Error vs t.Fatal 차이:
//   - t.Error: 실패로 표시하지만 나머지 테스트는 계속 실행
//   - t.Fatal: 즉시 테스트 중단
//
// 여기서는 t.Error를 써서 모든 위반을 다 보여준 뒤에 실패시킨다.
func reportViolations(t *testing.T, violations []report.Violation) {
	t.Helper()
	for _, v := range violations {
		if v.Severity == report.Error {
			t.Error(v.String()) // ERROR: 테스트 실패로 표시
		} else {
			t.Log(v.String()) // WARNING: 로그에만 출력
		}
	}
}
