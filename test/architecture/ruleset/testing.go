// testing.go — 테스트 품질 규칙을 정의한다.
//
// 4가지 규칙을 검사한다:
//
//   - testing/missing-goleak: 테스트 파일이 있는 패키지에 goleak이 적용되어 있는지 확인
//   - testing/missing-testify: 테스트 함수가 있는 파일에 testify를 사용하는지 확인
//   - testing/missing-build-tag: 테스트 파일에 //go:build unit 또는 //go:build e2e 태그가 있는지 확인
//   - testing/raw-assertion: testify를 import했지만 t.Fatal/t.Error 등 표준 단언도 함께 사용하는지 확인
//
// goleak(go.uber.org/goleak)은 Uber가 만든 goroutine 누수 검출 도구다.
// 테스트가 끝난 후에도 정리되지 않은 goroutine이 남아 있으면 테스트를 실패시킨다.
//
// testify(github.com/stretchr/testify)는 Go의 표준 testing 패키지를 보완하는 단언 라이브러리다.
// require.NoError(t, err)처럼 깔끔한 단언과, 실패 시 상세한 diff 출력을 제공한다.
//
// 이 규칙은 internal/ 아래의 모든 테스트 패키지에 두 도구의 적용을 강제한다.
package ruleset

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"rest-api/test/architecture/report"
)

// 테스트 품질 규칙에서 사용하는 import 경로 상수.
const goleakImportPath = "go.uber.org/goleak"

// testifyImportPrefixes는 testify 패키지의 import 경로 접두사 목록이다.
// require, assert, suite 중 하나라도 import하면 testify를 사용하는 것으로 판단한다.
//
//nolint:gochecknoglobals // 테스트 품질 규칙에서 사용하는 불변 설정값이다.
var testifyImportPrefixes = []string{
	"github.com/stretchr/testify/require",
	"github.com/stretchr/testify/assert",
	"github.com/stretchr/testify/suite",
}

// rawAssertionMethods는 testify로 대체해야 하는 testing.T의 단언 메서드 목록이다.
// 이 메서드들을 직접 호출하면 testify와 일관성이 깨지므로 사용을 금지한다.
//
//   - Fatal, Fatalf → require.NoError, require.FailNow 등으로 대체
//   - Error, Errorf → assert.NoError, assert.Fail 등으로 대체
//
// t.Log, t.Logf, t.Skip, t.Cleanup, t.Run 등은 testify가 대체하지 않는
// 테스트 인프라 메서드이므로 금지 대상이 아니다.
//
//nolint:gochecknoglobals // 테스트 품질 규칙에서 사용하는 불변 설정값이다.
var rawAssertionMethods = map[string]bool{
	"Fatal":  true,
	"Fatalf": true,
	"Error":  true,
	"Errorf": true,
}

// testPackageInfo는 하나의 테스트 패키지(디렉토리)에 대한 분석 결과를 담는다.
// goleak 규칙은 패키지 단위로 검사한다 (TestMain은 패키지당 하나).
type testPackageInfo struct {
	dir          string // 패키지 디렉토리의 절대 경로
	hasTestFiles bool   // _test.go 파일이 하나라도 있는지
	hasTestMain  bool   // TestMain 함수가 정의되어 있는지
	hasGoleak    bool   // goleak 패키지를 import하는지
}

// testFileInfo는 하나의 테스트 파일에 대한 분석 결과를 담는다.
// testify 규칙과 raw-assertion 규칙은 파일 단위로 검사한다.
type testFileInfo struct {
	path             string // 파일의 절대 경로
	hasTestFn        bool   // TestMain이 아닌 Test* 함수가 있는지
	hasTestify       bool   // testify 패키지를 import하는지
	hasRawAssertions bool   // t.Fatal, t.Fatalf, t.Error, t.Errorf 등 표준 단언을 직접 호출하는지
	hasBuildTag      bool   // //go:build unit 또는 //go:build e2e 태그가 있는지
}

// CheckTestingPatterns는 테스트 품질 규칙을 검사하여 위반 목록을 반환한다.
//
// 검사 대상: internal/ 아래의 모든 Go 테스트 파일 (_test.go)
//
// 검사 내용:
//  1. (패키지 단위) 테스트 파일이 있는 패키지에 TestMain + goleak이 있는지
//  2. (파일 단위) Test* 함수가 있는 파일에 testify를 import하는지
//  3. (파일 단위) 모든 테스트 파일에 //go:build unit 또는 //go:build e2e 태그가 있는지
//  4. (파일 단위) testify를 import한 파일에서 t.Fatal/t.Error 등 표준 단언을 혼용하지 않는지
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

	// internal/ 아래의 모든 테스트 패키지와 파일을 수집한다.
	packages, files := collectTestInfo(internalDir)

	// ── 규칙 1: goleak (패키지 단위) ──
	for _, pkg := range packages {
		if !pkg.hasTestFiles {
			continue
		}

		relDir, err := filepath.Rel(cfg.ProjectRoot, pkg.dir)
		if err != nil {
			continue
		}

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

	// ── 규칙 2: testify (파일 단위) ──
	for _, file := range files {
		if !file.hasTestFn {
			continue // Test* 함수가 없는 파일은 검사 대상이 아님 (TestMain만 있는 파일 등)
		}

		if !file.hasTestify {
			relPath, err := filepath.Rel(cfg.ProjectRoot, file.path)
			if err != nil {
				continue
			}
			violations = append(violations, report.Violation{
				Rule:     "testing/missing-testify",
				Severity: report.Error,
				Message:  "Test* 함수가 있지만 testify를 사용하지 않는다",
				File:     relPath,
				Fix:      "testify/require 또는 testify/assert를 import하여 단언문을 작성하라",
			})
		}
	}

	// ── 규칙 3: 빌드 태그 (파일 단위) ──
	// 모든 _test.go 파일에 //go:build unit 또는 //go:build e2e 태그가 있어야 한다.
	// 빌드 태그가 없으면 `go test ./...`에서 의도치 않게 실행될 수 있다.
	// 이 프로젝트는 unit/e2e 테스트를 분리 실행하므로, 태그 누락은 반드시 막아야 한다.
	for _, file := range files {
		if file.hasBuildTag {
			continue
		}

		relPath, err := filepath.Rel(cfg.ProjectRoot, file.path)
		if err != nil {
			continue
		}
		violations = append(violations, report.Violation{
			Rule:     "testing/missing-build-tag",
			Severity: report.Error,
			Message:  "테스트 파일에 //go:build unit 또는 //go:build e2e 태그가 없다",
			File:     relPath,
			Fix:      "파일 첫 줄에 //go:build unit 또는 //go:build e2e를 추가하라",
		})
	}

	// ── 규칙 4: raw assertion (파일 단위) ──
	// testify를 import했지만 t.Fatal, t.Error 등 표준 단언도 함께 사용하는 혼용을 잡는다.
	// testify를 import하지 않은 경우는 규칙 2(missing-testify)에서 이미 잡으므로 여기서는 제외한다.
	for _, file := range files {
		if !file.hasTestFn || !file.hasTestify || !file.hasRawAssertions {
			continue
		}

		relPath, err := filepath.Rel(cfg.ProjectRoot, file.path)
		if err != nil {
			continue
		}
		violations = append(violations, report.Violation{
			Rule:     "testing/raw-assertion",
			Severity: report.Error,
			Message:  "testify를 import했지만 t.Fatal/t.Error 등 표준 단언도 함께 사용한다",
			File:     relPath,
			Fix:      "t.Fatal → require.FailNow, t.Error → assert.Fail 등 testify 단언으로 교체하라",
		})
	}

	return violations
}

// collectTestInfo는 rootDir 아래의 모든 디렉토리를 순회하면서
// 패키지 단위 정보(goleak용)와 파일 단위 정보(testify용)를 동시에 수집한다.
func collectTestInfo(rootDir string) ([]testPackageInfo, []testFileInfo) {
	pkgMap := make(map[string]*testPackageInfo)
	var files []testFileInfo

	//nolint:errcheck,gosec // WalkDir 콜백 내에서 에러를 개별 처리한다 (건너뛰기).
	filepath.WalkDir(rootDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil //nolint:nilerr // 접근 에러가 발생한 항목은 건너뛰고 순회를 계속한다.
		}

		if d.IsDir() {
			name := d.Name()
			if strings.HasPrefix(name, ".") || name == "vendor" || name == "tmp" {
				return filepath.SkipDir
			}
			return nil
		}

		if !strings.HasSuffix(path, "_test.go") {
			return nil
		}

		dir := filepath.Dir(path)

		// 패키지 정보 초기화
		pkg, exists := pkgMap[dir]
		if !exists {
			pkg = &testPackageInfo{dir: dir}
			pkgMap[dir] = pkg
		}
		pkg.hasTestFiles = true

		// 파일을 AST로 파싱하여 패키지/파일 정보를 동시에 수집
		fileInfo := analyzeTestFile(path, pkg)
		files = append(files, fileInfo)

		return nil
	})

	packages := make([]testPackageInfo, 0, len(pkgMap))
	for _, pkg := range pkgMap {
		packages = append(packages, *pkg)
	}
	return packages, files
}

// analyzeTestFile은 하나의 _test.go 파일을 AST로 파싱하여
// 패키지 정보(TestMain, goleak)와 파일 정보(Test* 함수, testify)를 수집한다.
//
// go/parser.ParseFile로 파일을 파싱하고:
//   - import 목록에서 goleak, testify 경로를 확인
//   - 최상위 함수 선언에서 TestMain, Test* 함수를 확인
//
// 패키지 정보는 pkg 포인터에 직접 기록하고, 파일 정보는 testFileInfo로 반환한다.
func analyzeTestFile(path string, pkg *testPackageInfo) testFileInfo {
	info := testFileInfo{path: path}

	fset := token.NewFileSet()
	// parser.ParseComments: 주석도 AST에 포함시킨다.
	// 빌드 태그(//go:build ...)는 주석으로 표현되므로, 주석 파싱이 필수다.
	astFile, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	if err != nil {
		return info
	}

	// 빌드 태그 검사: //go:build 주석에 unit 또는 e2e가 포함되어 있는지 확인한다.
	// Go 1.17+에서 //go:build는 빌드 제약 조건(build constraint)을 지정하는 공식 문법이다.
	// 이 프로젝트에서는 모든 테스트 파일이 unit 또는 e2e로 분류되어야 한다.
	info.hasBuildTag = detectBuildTag(astFile)

	// import 목록을 검사한다
	for _, imp := range astFile.Imports {
		importPath := strings.Trim(imp.Path.Value, `"`)

		// goleak import 확인
		if importPath == goleakImportPath {
			pkg.hasGoleak = true
		}

		// testify import 확인
		// require, assert, suite 중 하나라도 import하면 testify 사용으로 판단
		if slices.Contains(testifyImportPrefixes, importPath) {
			info.hasTestify = true
		}
	}

	// 최상위 함수 선언을 검사한다
	for _, decl := range astFile.Decls {
		funcDecl, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}

		// 리시버가 없는(일반 함수) 것만 확인
		// 메서드(리시버가 있는 함수)는 suite의 Test* 메서드이므로
		// suite를 import했으면 이미 testify를 사용하는 것이다.
		if funcDecl.Recv != nil {
			continue
		}

		name := funcDecl.Name.Name

		if name == "TestMain" {
			pkg.hasTestMain = true
		} else if strings.HasPrefix(name, "Test") {
			info.hasTestFn = true
		}
	}

	// 함수 본문에서 표준 라이브러리의 단언 메서드(t.Fatal 등) 사용을 검사한다.
	// testify와의 혼용을 방지하기 위해, testing.T 파라미터에 대한
	// Fatal, Fatalf, Error, Errorf 호출을 감지한다.
	info.hasRawAssertions = detectRawAssertions(astFile)

	return info
}

// detectRawAssertions는 파일에서 testing.T의 단언 메서드 직접 호출을 감지한다.
//
// testify를 사용하는 프로젝트에서 t.Fatal, t.Fatalf, t.Error, t.Errorf를
// 직접 호출하면 일관성이 깨지므로, 이 함수로 혼용을 감지한다.
//
// 감지 방식:
//  1. 파일 내 모든 함수(FuncDecl, FuncLit 포함)에서 *testing.T 파라미터 이름을 수집한다
//  2. 파일 전체를 AST로 순회하면서 해당 파라미터의 금지된 메서드 호출을 찾는다
//
// FuncLit(익명 함수)도 검사하므로 t.Run() 콜백 내부의 호출도 잡아낸다.
// 예: t.Run("sub", func(t *testing.T) { t.Fatal("...") })
func detectRawAssertions(astFile *ast.File) bool {
	// 1단계: 파일 내 모든 함수에서 *testing.T 파라미터 이름을 수집한다.
	// 관례적으로 t를 사용하지만, tt, testingT 등 다른 이름도 잡기 위해 동적으로 수집한다.
	tNames := make(map[string]bool)
	ast.Inspect(astFile, func(n ast.Node) bool {
		switch fn := n.(type) {
		case *ast.FuncDecl:
			// 일반 함수 선언 (예: func TestFoo(t *testing.T))
			if name := findTestingTParam(fn.Type.Params); name != "" {
				tNames[name] = true
			}
		case *ast.FuncLit:
			// 익명 함수 (예: t.Run("sub", func(t *testing.T) { ... }))
			if name := findTestingTParam(fn.Type.Params); name != "" {
				tNames[name] = true
			}
		}
		return true
	})

	if len(tNames) == 0 {
		return false
	}

	// 2단계: 파일 전체에서 금지된 메서드 호출을 검색한다.
	// t.Fatal(), t.Errorf() 등의 패턴을 AST에서 찾는다.
	//
	// AST 구조:
	//   *ast.CallExpr (함수 호출)
	//     └─ Fun: *ast.SelectorExpr (메서드 선택)
	//           ├─ X: *ast.Ident (수신자, 예: "t")
	//           └─ Sel: *ast.Ident (메서드명, 예: "Fatal")
	found := false
	ast.Inspect(astFile, func(n ast.Node) bool {
		if found {
			return false // 이미 발견했으면 순회를 중단한다
		}

		callExpr, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}

		selExpr, ok := callExpr.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}

		ident, ok := selExpr.X.(*ast.Ident)
		if !ok {
			return true
		}

		// 수집한 *testing.T 파라미터 이름과 금지된 메서드인지 동시에 확인한다.
		// 예: t.Fatal → tNames["t"]=true && rawAssertionMethods["Fatal"]=true
		if tNames[ident.Name] && rawAssertionMethods[selExpr.Sel.Name] {
			found = true
			return false
		}

		return true
	})

	return found
}

// findTestingTParam는 함수 파라미터 목록에서 *testing.T 타입 파라미터의 이름을 반환한다.
//
// 예:
//
//	func TestFoo(t *testing.T) → "t"
//	func helper(tt *testing.T, ...) → "tt"
//	func noTest(s string) → ""
//
// AST에서 *testing.T는 다음 구조로 표현된다:
//
//	*ast.StarExpr (포인터)
//	  └─ *ast.SelectorExpr (패키지.타입)
//	        ├─ X: *ast.Ident (패키지명: "testing")
//	        └─ Sel: *ast.Ident (타입명: "T")
func findTestingTParam(params *ast.FieldList) string {
	if params == nil {
		return ""
	}

	for _, param := range params.List {
		// *testing.T 형태인지 확인: StarExpr → SelectorExpr
		starExpr, ok := param.Type.(*ast.StarExpr)
		if !ok {
			continue
		}

		selExpr, ok := starExpr.X.(*ast.SelectorExpr)
		if !ok {
			continue
		}

		ident, ok := selExpr.X.(*ast.Ident)
		if !ok {
			continue
		}

		// 패키지가 "testing"이고 타입이 "T"인지 확인한다
		if ident.Name == "testing" && selExpr.Sel.Name == "T" {
			if len(param.Names) > 0 {
				return param.Names[0].Name
			}
		}
	}

	return ""
}

// allowedBuildTags는 이 프로젝트에서 허용하는 빌드 태그 목록이다.
// 모든 _test.go 파일은 이 중 하나를 //go:build 주석으로 선언해야 한다.
//
//nolint:gochecknoglobals // 테스트 품질 규칙에서 사용하는 불변 설정값이다.
var allowedBuildTags = []string{"unit", "e2e"}

// detectBuildTag는 파일의 주석에서 //go:build 빌드 태그를 찾아
// unit 또는 e2e 중 하나가 포함되어 있는지 확인한다.
//
// Go 1.17+에서 빌드 제약 조건은 //go:build 주석으로 지정한다.
// 예: //go:build unit
//
// AST에서 주석은 f.Comments에 []*ast.CommentGroup으로 저장된다.
// 각 CommentGroup은 연속된 주석 줄들의 묶음이고,
// 그 안에 Comment.Text가 개별 주석 문자열이다.
// 예: "//go:build unit\n" → Text는 "//go:build unit"
func detectBuildTag(f *ast.File) bool {
	for _, cg := range f.Comments {
		for _, c := range cg.List {
			text := c.Text
			// //go:build 접두사로 시작하는 주석만 검사한다.
			if !strings.HasPrefix(text, "//go:build ") {
				continue
			}

			// //go:build 뒤의 제약 조건 문자열을 추출한다.
			// 예: "//go:build unit" → "unit"
			// 예: "//go:build unit && !race" → "unit && !race"
			constraint := strings.TrimPrefix(text, "//go:build ")

			// 허용된 태그(unit, e2e) 중 하나가 제약 조건에 포함되어 있는지 확인한다.
			// strings.Contains를 사용하므로 "unit && !race" 같은 복합 조건도 통과한다.
			for _, tag := range allowedBuildTags {
				if strings.Contains(constraint, tag) {
					return true
				}
			}
		}
	}

	return false
}
