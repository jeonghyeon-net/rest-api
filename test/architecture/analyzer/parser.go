// Package analyzer는 Go 소스 코드를 분석(파싱)하는 패키지다.
//
// Go 표준 라이브러리의 go/ast(Abstract Syntax Tree, 추상 구문 트리) 패키지를 사용해서
// .go 파일을 읽고, 그 안에 있는 import문, 타입 선언, 함수 선언 등의 정보를 추출한다.
//
// AST란?
//
//	소스 코드를 트리 구조로 표현한 것이다.
//	예를 들어 "func Add(a, b int) int" 라는 코드는 AST에서
//	FuncDecl(함수선언) 노드가 되고, 그 아래에 이름(Add), 파라미터(a,b), 반환타입(int) 등이 달린다.
//	이 트리를 탐색하면 코드의 구조를 프로그래밍적으로 분석할 수 있다.
package analyzer

import (
	"fmt"           // 포맷팅된 문자열 생성
	"go/ast"        // Go AST 노드 타입들이 정의된 패키지
	"go/parser"     // Go 소스코드를 AST로 파싱하는 패키지
	"go/token"      // 소스코드 내 위치(줄번호 등)를 추적하는 패키지
	"os"            // 파일/디렉토리 조작
	"path/filepath" // 파일 경로 조작 (OS에 맞게 경로 처리)
	"strings"       // 문자열 처리 유틸리티
)

// ──────────────────────────────────────────────
// 데이터 구조체 정의
// ──────────────────────────────────────────────

// FileInfo는 하나의 Go 파일을 분석한 결과를 담는 구조체다.
// 파일 경로, 패키지명, import 목록, 타입 목록, 함수 목록을 포함한다.
type FileInfo struct {
	Path      string       // 파일의 절대 경로 (예: /project/internal/domain/user/model/user.go)
	Package   string       // 파일의 package 선언 이름 (예: "model")
	Imports   []ImportInfo // 이 파일이 import하는 패키지 목록
	Types     []TypeInfo   // 이 파일에 선언된 타입(struct, interface 등) 목록
	Functions []FuncInfo   // 이 파일에 선언된 함수/메서드 목록
}

// ImportInfo는 하나의 import 문 정보를 담는 구조체다.
//
// 예시:
//
//	import userModel "rest-api/internal/domain/user/model"
//	→ Alias: "userModel", Path: "rest-api/internal/domain/user/model", Line: 3
//
//	import "fmt"
//	→ Alias: "", Path: "fmt", Line: 4
type ImportInfo struct {
	Alias string // import 별칭 (없으면 빈 문자열). 예: import myAlias "some/package"에서 "myAlias"
	Path  string // import 경로. 예: "rest-api/internal/domain/user/model"
	Line  int    // 소스코드에서의 줄 번호
}

// TypeInfo는 타입 선언 하나의 정보를 담는 구조체다.
//
// Go에서 타입은 크게 두 가지:
//   - struct(구조체): 데이터를 묶어놓은 것. 예: type User struct { Name string }
//   - interface(인터페이스): 메서드 집합을 정의한 것. 예: type Reader interface { Read() }
type TypeInfo struct {
	Name        string // 타입 이름 (예: "User", "Repository")
	IsExported  bool   // 대문자로 시작하면 true (외부 패키지에서 접근 가능). 예: User=true, user=false
	IsInterface bool   // interface 타입이면 true, struct 등이면 false
	Line        int    // 소스코드에서의 줄 번호
}

// FuncInfo는 함수 또는 메서드 하나의 정보를 담는 구조체다.
//
// 함수 vs 메서드:
//   - 함수: func DoSomething() { ... }  ← Receiver가 빈 문자열
//   - 메서드: func (u *User) DoSomething() { ... }  ← Receiver가 "*User"
type FuncInfo struct {
	Name        string   // 함수/메서드 이름 (예: "New", "GetUser")
	Receiver    string   // 메서드의 리시버 타입 (함수면 빈 문자열). 예: "*user", "user"
	ReturnTypes []string // 반환 타입 목록. 예: ["User", "error"]
	Line        int      // 소스코드에서의 줄 번호
	IsExported  bool     // 대문자로 시작하면 true
}

// ──────────────────────────────────────────────
// 파일 파싱 함수
// ──────────────────────────────────────────────

// ParseFile은 하나의 Go 파일을 읽어서 AST로 파싱하고, FileInfo로 변환한다.
//
// 동작 순서:
//  1. go/parser로 파일을 AST(추상 구문 트리)로 변환
//  2. AST를 순회하면서 import, type, func 선언을 추출
//  3. 추출한 정보를 FileInfo 구조체에 담아 반환
func ParseFile(path string) (*FileInfo, error) {
	// token.FileSet은 파일 내 위치(줄번호, 열번호)를 추적하기 위한 객체다.
	// AST 노드에서 "이 노드는 소스코드 몇 번째 줄에 있나?" 를 알려면 이게 필요하다.
	fset := token.NewFileSet()

	// parser.ParseFile: Go 파일을 읽어서 AST로 변환한다.
	// 반환값 f는 *ast.File 타입으로, 파일 전체의 AST 트리 루트 노드다.
	// 마지막 인자 0은 파싱 옵션인데, 0이면 기본 동작(주석은 무시)한다.
	// astFile은 파싱된 Go 파일의 AST 루트 노드다.
	// 변수명을 astFile로 지어서 ast.File 타입임을 명확히 한다.
	astFile, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		return nil, fmt.Errorf("파일 파싱 실패 %s: %w", path, err) // 파싱 실패시 에러 반환
	}

	// 결과를 담을 FileInfo 구조체 생성
	// astFile.Name.Name은 파일 최상단의 "package xxx" 에서 xxx 부분이다.
	info := &FileInfo{
		Path:    path,
		Package: astFile.Name.Name,
	}

	// ── import 문 추출 ──
	// astFile.Imports는 이 파일의 모든 import 문 슬라이스다.
	for _, imp := range astFile.Imports {
		ii := ImportInfo{
			// imp.Path.Value는 큰따옴표가 포함된 문자열이다. 예: "\"fmt\""
			// strings.Trim으로 양쪽의 큰따옴표를 제거한다.
			Path: strings.Trim(imp.Path.Value, `"`),
			// fset.Position()은 AST 노드의 소스코드 내 위치(줄번호 등)를 알려준다.
			Line: fset.Position(imp.Pos()).Line,
		}
		// import에 별칭(alias)이 있으면 저장한다.
		// 예: import myAlias "some/package" 에서 myAlias
		if imp.Name != nil {
			ii.Alias = imp.Name.Name
		}
		info.Imports = append(info.Imports, ii)
	}

	// ── 타입 선언과 함수 선언 추출 ──
	// astFile.Decls는 파일의 모든 최상위 선언(declaration) 목록이다.
	// Go에서 최상위 선언은 크게 두 종류:
	//   1. GenDecl: type, var, const 등의 일반 선언
	//   2. FuncDecl: func 선언 (함수 또는 메서드)
	for _, decl := range astFile.Decls {
		// switch declNode := decl.(type)는 "타입 스위치"라고 한다.
		// decl의 실제 타입이 무엇인지에 따라 분기한다.
		switch declNode := decl.(type) {
		// GenDecl: type, var, const 등의 일반 선언
		case *ast.GenDecl:
			// GenDecl 안에는 여러 개의 Spec(사양)이 들어있을 수 있다.
			// 예: type ( A struct{}; B interface{} ) 처럼 괄호로 묶인 경우
			for _, spec := range declNode.Specs {
				// TypeSpec만 관심 있다 (var, const는 건너뜀)
				// ts, ok := spec.(*ast.TypeSpec) 는 "타입 단언(type assertion)"이다.
				// spec이 *ast.TypeSpec 타입이면 ok=true, 아니면 ok=false
				ts, ok := spec.(*ast.TypeSpec)
				if !ok {
					continue // TypeSpec이 아니면 건너뜀
				}

				ti := TypeInfo{
					Name:       ts.Name.Name,         // 타입 이름
					IsExported: ts.Name.IsExported(), // 대문자 시작 여부
					Line:       fset.Position(ts.Pos()).Line,
				}

				// ts.Type가 *ast.InterfaceType이면 인터페이스 타입이다.
				if _, ok := ts.Type.(*ast.InterfaceType); ok {
					ti.IsInterface = true
				}

				info.Types = append(info.Types, ti)
			}

		// FuncDecl: 함수 또는 메서드 선언
		case *ast.FuncDecl:
			fi := FuncInfo{
				Name:       declNode.Name.Name,
				IsExported: declNode.Name.IsExported(),
				Line:       fset.Position(declNode.Pos()).Line,
			}

			// 리시버(Receiver) 확인: 리시버가 있으면 메서드, 없으면 일반 함수
			// 예: func (u *User) GetName() 에서 u *User 부분이 리시버
			if declNode.Recv != nil && len(declNode.Recv.List) > 0 {
				fi.Receiver = exprToString(declNode.Recv.List[0].Type)
			}

			// 반환 타입 추출
			// 예: func Foo() (User, error) → ReturnTypes: ["User", "error"]
			if declNode.Type.Results != nil {
				for _, result := range declNode.Type.Results.List {
					fi.ReturnTypes = append(fi.ReturnTypes, exprToString(result.Type))
				}
			}

			info.Functions = append(info.Functions, fi)
		}
	}

	return info, nil
}

// exprToString은 AST의 타입 표현(ast.Expr)을 사람이 읽을 수 있는 문자열로 변환한다.
//
// AST에서 타입은 여러 형태의 노드로 표현된다:
//   - *ast.Ident: 단순 이름. 예: "User", "string", "int"
//   - *ast.StarExpr: 포인터. 예: *User → "*User"
//   - *ast.SelectorExpr: 패키지.타입. 예: model.User → "model.User"
//   - *ast.ArrayType: 슬라이스/배열. 예: []User → "[]User"
//   - *ast.MapType: 맵. 예: map[string]int → "map[string]int"
//
// 이 함수는 재귀적으로(자기 자신을 호출하면서) 중첩된 타입도 처리한다.
// 예: []*model.User → "[]" + "*" + "model.User"
func exprToString(expr ast.Expr) string {
	switch node := expr.(type) {
	case *ast.Ident:
		// 단순 식별자: User, string, int 등
		return node.Name
	case *ast.StarExpr:
		// 포인터 타입: *Something
		return "*" + exprToString(node.X) // node.X는 * 뒤의 타입
	case *ast.SelectorExpr:
		// 패키지.타입: model.User 에서 model은 X, User는 Sel
		return exprToString(node.X) + "." + node.Sel.Name
	case *ast.ArrayType:
		// 슬라이스/배열: []Something
		return "[]" + exprToString(node.Elt) // node.Elt는 요소 타입
	case *ast.MapType:
		// 맵: map[Key]Value
		return "map[" + exprToString(node.Key) + "]" + exprToString(node.Value)
	default:
		// 알 수 없는 타입은 빈 문자열 반환
		return ""
	}
}

// ──────────────────────────────────────────────
// 디렉토리 파싱 함수
// ──────────────────────────────────────────────

// ParseDirectory는 주어진 디렉토리 아래의 모든 Go 파일을 재귀적으로 찾아서 파싱한다.
//
// 동작:
//  1. filepath.Walk로 디렉토리를 재귀 순회
//  2. 숨김 디렉토리(.git 등), vendor, tmp 디렉토리는 건너뜀
//  3. _test.go 파일은 건너뜀 (테스트 파일은 분석 대상이 아님)
//  4. 나머지 .go 파일을 ParseFile로 분석하여 결과를 모은다
func ParseDirectory(root string) ([]*FileInfo, error) {
	// 디렉토리가 존재하지 않으면 빈 결과를 반환한다 (에러 아님).
	// 프로젝트 초기에 internal/ 디렉토리가 아직 없을 수 있기 때문이다.
	if _, err := os.Stat(root); os.IsNotExist(err) {
		return nil, nil
	}

	var files []*FileInfo

	// filepath.Walk는 디렉토리 트리를 재귀적으로 순회하면서
	// 각 파일/디렉토리마다 전달한 함수(콜백)를 호출한다.
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// 디렉토리인 경우: 특정 디렉토리는 건너뛴다
		if info.IsDir() {
			name := info.Name()
			// filepath.SkipDir를 반환하면 해당 디렉토리를 통째로 건너뛴다.
			if strings.HasPrefix(name, ".") || name == "vendor" || name == "tmp" {
				return filepath.SkipDir
			}
			return nil // 이 디렉토리 안으로 진입
		}

		// 파일인 경우: .go 파일만 분석하고, _test.go는 제외
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil // 건너뜀
		}

		// Go 파일을 파싱한다
		fi, parseErr := ParseFile(path)
		if parseErr != nil {
			return parseErr
		}
		files = append(files, fi)
		return nil
	})
	if err != nil {
		return files, fmt.Errorf("디렉토리 순회 실패 %s: %w", root, err)
	}

	return files, nil
}
