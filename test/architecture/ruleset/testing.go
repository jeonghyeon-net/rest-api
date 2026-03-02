// testing.go — 테스트 품질 규칙을 정의한다.
//
// 현재 1가지 규칙을 검사한다:
//
//   - testing/missing-goleak: 테스트 파일이 있는 패키지에 goleak이 적용되어 있는지 확인
//
// goleak(go.uber.org/goleak)은 Uber가 만든 goroutine 누수 검출 도구다.
// 테스트가 끝난 후에도 정리되지 않은 goroutine이 남아 있으면 테스트를 실패시킨다.
// DB 연결, 타이머, 채널 등에서 발생하는 goroutine 누수를 테스트 시점에 잡아낸다.
//
// goleak을 적용하려면 패키지에 TestMain 함수를 만들고
// goleak.VerifyTestMain(m)을 호출해야 한다:
//
//	func TestMain(m *testing.M) {
//	    goleak.VerifyTestMain(m)
//	}
//
// 이 규칙은 internal/ 아래의 모든 테스트 패키지에 goleak 적용을 강제한다.
package ruleset

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"

	"rest-api/test/architecture/report"
)

// goleakImportPath는 goleak 패키지의 import 경로다.
const goleakImportPath = "go.uber.org/goleak"

// testPackageInfo는 하나의 테스트 패키지(디렉토리)에 대한 분석 결과를 담는다.
type testPackageInfo struct {
	dir          string // 패키지 디렉토리의 절대 경로
	hasTestFiles bool   // _test.go 파일이 하나라도 있는지
	hasTestMain  bool   // TestMain 함수가 정의되어 있는지
	hasGoleak    bool   // goleak 패키지를 import하는지
}

// CheckTestingPatterns는 테스트 품질 규칙을 검사하여 위반 목록을 반환한다.
//
// 검사 대상: internal/ 아래의 모든 Go 테스트 파일 (_test.go)
//
// 검사 내용:
//  1. 테스트 파일이 있는 패키지에 TestMain 함수가 있는지
//  2. TestMain이 goleak.VerifyTestMain을 호출하는지 (import 여부로 판단)
//
// 테스트 파일을 AST로 파싱하여 함수 선언과 import 문을 검사한다.
// 기존 analyzer.ParseDirectory는 _test.go를 건너뛰므로, 여기서 별도로 파싱한다.
func CheckTestingPatterns(cfg *Config) []report.Violation {
	var violations []report.Violation

	internalDir := filepath.Join(cfg.ProjectRoot, "internal")

	// internal/ 디렉토리가 없으면 검사할 것이 없다 (프로젝트 초기)
	if _, err := os.Stat(internalDir); os.IsNotExist(err) {
		return nil
	}

	// internal/ 아래의 모든 테스트 패키지를 수집한다.
	packages := collectTestPackages(internalDir)

	for _, pkg := range packages {
		if !pkg.hasTestFiles {
			continue // 테스트 파일이 없는 패키지는 검사 대상이 아님
		}

		// 프로젝트 루트 기준 상대 경로로 변환 (에러 메시지 가독성)
		relDir, _ := filepath.Rel(cfg.ProjectRoot, pkg.dir)

		if !pkg.hasTestMain {
			violations = append(violations, report.Violation{
				Rule:     "testing/missing-goleak",
				Severity: report.Error,
				Message:  "테스트 파일이 있는 패키지에 TestMain + goleak.VerifyTestMain이 없다",
				File:     relDir,
				Fix:      "해당 패키지에 TestMain 함수를 추가하고 goleak.VerifyTestMain(m)을 호출하라",
			})
		} else if !pkg.hasGoleak {
			violations = append(violations, report.Violation{
				Rule:     "testing/missing-goleak",
				Severity: report.Error,
				Message:  "TestMain이 있지만 goleak.VerifyTestMain을 호출하지 않는다",
				File:     relDir,
				Fix:      "TestMain에서 goleak.VerifyTestMain(m)을 호출하라 (go.uber.org/goleak import 필요)",
			})
		}
	}

	return violations
}

// collectTestPackages는 rootDir 아래의 모든 디렉토리를 순회하면서
// 각 디렉토리(패키지)의 테스트 파일 정보를 수집한다.
//
// 반환값은 디렉토리별로 하나의 testPackageInfo를 담은 슬라이스다.
// 같은 디렉토리의 여러 _test.go 파일은 하나의 testPackageInfo로 합쳐진다.
func collectTestPackages(rootDir string) []testPackageInfo {
	// map[디렉토리경로]*testPackageInfo 로 디렉토리별 정보를 모은다.
	pkgMap := make(map[string]*testPackageInfo)

	// filepath.WalkDir은 filepath.Walk보다 효율적인 디렉토리 순회 함수다.
	// os.DirEntry를 사용하여 불필요한 os.Stat 호출을 줄인다.
	_ = filepath.WalkDir(rootDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // 에러가 있어도 계속 진행
		}

		// 숨김 디렉토리, vendor, tmp는 건너뛴다
		if d.IsDir() {
			name := d.Name()
			if strings.HasPrefix(name, ".") || name == "vendor" || name == "tmp" {
				return filepath.SkipDir
			}
			return nil
		}

		// _test.go 파일만 관심 있다
		if !strings.HasSuffix(path, "_test.go") {
			return nil
		}

		dir := filepath.Dir(path)

		// 이 디렉토리의 testPackageInfo가 없으면 생성
		pkg, exists := pkgMap[dir]
		if !exists {
			pkg = &testPackageInfo{dir: dir}
			pkgMap[dir] = pkg
		}
		pkg.hasTestFiles = true

		// 이미 goleak이 발견된 패키지는 추가 파싱 불필요
		if pkg.hasGoleak {
			return nil
		}

		// _test.go 파일을 AST로 파싱하여 TestMain과 goleak import를 찾는다
		analyzeTestFile(path, pkg)

		return nil
	})

	// map을 슬라이스로 변환하여 반환
	result := make([]testPackageInfo, 0, len(pkgMap))
	for _, pkg := range pkgMap {
		result = append(result, *pkg)
	}
	return result
}

// analyzeTestFile은 하나의 _test.go 파일을 AST로 파싱하여
// TestMain 함수 존재 여부와 goleak import 여부를 확인한다.
//
// go/parser.ParseFile로 파일을 파싱하고:
//   - import 목록에서 "go.uber.org/goleak"이 있는지 확인
//   - 최상위 함수 선언 중 "TestMain"이 있는지 확인
//
// 결과는 pkg 포인터에 직접 기록한다 (반환값 없음).
func analyzeTestFile(path string, pkg *testPackageInfo) {
	fset := token.NewFileSet()

	// parser.ImportsOnly 플래그를 사용하면 import 문만 파싱하여 빠르다.
	// 하지만 TestMain 함수 선언도 확인해야 하므로 전체 파싱(0)을 한다.
	f, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		return // 파싱 실패한 파일은 건너뜀
	}

	// import 목록에서 goleak 경로를 찾는다
	for _, imp := range f.Imports {
		importPath := strings.Trim(imp.Path.Value, `"`)
		if importPath == goleakImportPath {
			pkg.hasGoleak = true
			break
		}
	}

	// 최상위 선언에서 TestMain 함수를 찾는다
	for _, decl := range f.Decls {
		// 타입 단언으로 FuncDecl(함수 선언)인지 확인한다.
		// import, type, var 등의 GenDecl은 건너뛴다.
		funcDecl, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}

		// 함수 이름이 "TestMain"이고 리시버가 없는 경우 (메서드가 아닌 함수)
		if funcDecl.Name.Name == "TestMain" && funcDecl.Recv == nil {
			pkg.hasTestMain = true
			break
		}
	}
}
