// Package ruleset은 아키텍처 규칙들을 정의하고 검사하는 패키지다.
//
// 5가지 규칙 카테고리를 포함한다:
//   - dependency: 의존성 방향 규칙 (어떤 패키지가 어떤 패키지를 import할 수 있는지)
//   - naming: 네이밍 컨벤션 (패키지명, 타입명 규칙)
//   - interface: 인터페이스 패턴 (인터페이스 + 구현체 + 생성자 패턴)
//   - structure: 디렉토리 구조 규칙 (허용된 폴더만 사용)
//   - sqlc: 코드 생성 규칙 (sqlc 설정 완전성 + 수기 코드 차단)
package ruleset

import (
	"bufio" // 버퍼링된 I/O (파일을 줄 단위로 읽을 때 사용)
	"os"
	"path/filepath"
	"strings"
)

// Config는 아키텍처 테스트 실행에 필요한 프로젝트 설정 정보를 담는다.
type Config struct {
	ModuleName  string // go.mod에 정의된 모듈 이름. 예: "rest-api"
	ProjectRoot string // 프로젝트 루트 디렉토리의 절대 경로
}

// ──────────────────────────────────────────────
// 규칙에서 사용하는 상수 목록들
// ──────────────────────────────────────────────

var (
	// AllowedDomainDirs: 도메인 디렉토리 안에 허용되는 하위 디렉토리 목록.
	// 이 목록에 없는 디렉토리가 있으면 structure 규칙 위반이다.
	//
	// 예: internal/domain/user/ 아래에는 이것들만 있을 수 있다:
	//   subdomain/ - 서브도메인들
	//   svc/       - 공개 서비스 인터페이스
	//   handler/   - HTTP/gRPC 요청 핸들러
	//   infra/     - 외부 서비스 클라이언트
	AllowedDomainDirs = []string{"subdomain", "svc", "handler", "infra"}

	// AllowedSubdomainLayers: 서브도메인 안에 허용되는 레이어 디렉토리 목록.
	//
	// DDD에서 각 레이어의 역할:
	//   model/ - 도메인 모델 (엔티티, 값 객체). 비즈니스 규칙의 핵심.
	//   repo/  - 리포지토리 인터페이스. 데이터 저장/조회 방법을 추상화.
	//   svc/   - 서비스. 비즈니스 로직을 구현. model과 repo를 조합.
	AllowedSubdomainLayers = []string{"model", "repo", "svc"}

	// AllowedHandlerProtocols: handler/ 아래에 허용되는 프로토콜 디렉토리.
	//
	//   http/    - REST API 핸들러
	//   grpc/    - gRPC 핸들러
	//   jsonrpc/ - JSON-RPC 핸들러
	AllowedHandlerProtocols = []string{"http", "grpc", "jsonrpc"}

	// ForbiddenPackageNames: 사용이 금지된 패키지 이름 목록.
	// 이런 이름의 패키지는 "이 패키지가 뭘 하는지" 알 수 없어서 나쁜 패키지명이다.
	// 예를 들어 "util" 패키지에는 아무 기능이나 들어갈 수 있어서 점점 비대해진다.
	ForbiddenPackageNames = []string{"util", "utils", "common", "misc", "helper", "helpers", "shared", "lib"}

	// LayerOrder는 레이어 간 의존성 방향을 인덱스로 정의한다.
	// 인덱스가 작을수록 안쪽(의존성의 바닥) 레이어다.
	//
	// 의존성 규칙: 안쪽 레이어는 바깥쪽 레이어를 import할 수 없다.
	//   model(0) ← repo(1) ← svc(2)
	//
	// 즉:
	//   svc → repo ✓ (바깥에서 안쪽으로 = OK)
	//   svc → model ✓ (바깥에서 안쪽으로 = OK)
	//   repo → model ✓ (바깥에서 안쪽으로 = OK)
	//   model → repo ✗ (안쪽에서 바깥쪽으로 = 위반!)
	//   model → svc  ✗ (안쪽에서 바깥쪽으로 = 위반!)
	//   repo → svc   ✗ (안쪽에서 바깥쪽으로 = 위반!)
	LayerOrder = []string{"model", "repo", "svc"}
)

// ──────────────────────────────────────────────
// Config 생성 함수
// ──────────────────────────────────────────────

// NewConfig는 프로젝트 루트 경로를 받아서 Config를 생성한다.
// go.mod 파일에서 모듈 이름을 읽어온다.
func NewConfig(projectRoot string) (*Config, error) {
	moduleName, err := readModuleName(projectRoot)
	if err != nil {
		return nil, err
	}
	return &Config{
		ModuleName:  moduleName,
		ProjectRoot: projectRoot,
	}, nil
}

// readModuleName은 go.mod 파일을 열어서 "module xxx" 줄에서 모듈 이름을 추출한다.
//
// go.mod 파일 예시:
//
//	module rest-api        ← 이 줄에서 "rest-api"를 추출
//	go 1.25.0
func readModuleName(projectRoot string) (string, error) {
	f, err := os.Open(filepath.Join(projectRoot, "go.mod"))
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }() // 함수가 끝나면 파일을 닫는다 (defer = 지연 실행)

	// bufio.Scanner를 사용해서 파일을 줄 단위로 읽는다.
	scanner := bufio.NewScanner(f)
	for scanner.Scan() { // 한 줄씩 읽기
		line := strings.TrimSpace(scanner.Text()) // 앞뒤 공백 제거
		if strings.HasPrefix(line, "module ") {
			// "module " 접두사를 제거하면 모듈 이름만 남는다
			return strings.TrimSpace(strings.TrimPrefix(line, "module ")), nil
		}
	}
	return "", scanner.Err()
}
