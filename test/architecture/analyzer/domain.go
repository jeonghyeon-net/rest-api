package analyzer

// 이 파일은 Go 파일의 경로를 분석해서 프로젝트 구조상
// 어떤 도메인/서브도메인/레이어에 속하는지, 또는 Saga인지를 파악하는 역할을 한다.
//
// 두 가지 경로 패턴을 처리한다:
//
// 1. 도메인 파일: "internal/domain/user/subdomain/core/model/user.go"
//    → Domain:"user", Subdomain:"core", Layer:"model"
//
// 2. Saga 파일: "internal/saga/create_order/saga.go"
//    → IsSaga:true, SagaName:"create_order"
//
// Saga란?
//
//	여러 도메인에 걸친 트랜잭션을 조율(orchestrate)하는 패턴이다.
//	NestJS 비유: 여러 서비스를 조합하는 "Orchestrator" 또는 "Use Case" 클래스와 비슷.
//
//	예: "주문 생성" Saga = order 도메인(주문 생성) + payment 도메인(결제) + inventory 도메인(재고 차감)
//	이 3개 도메인의 Public Service를 호출해서 하나의 비즈니스 플로우로 엮는다.
//	하나가 실패하면 보상 트랜잭션(compensation)으로 롤백한다.

import (
	"path/filepath"
	"strings"
)

// DomainPath는 하나의 파일이 프로젝트 구조에서 어디에 위치하는지를 나타낸다.
//
// 도메인 파일과 Saga 파일 모두 이 하나의 구조체로 표현한다.
// IsSaga가 true면 Saga 파일이고, false면 도메인 파일이다.
//
// 프로젝트에서 기대하는 디렉토리 구조:
//
//	internal/
//	├── domain/{도메인}/                   ← 도메인 파일들
//	│   ├── subdomain/{서브도메인}/{레이어}/
//	│   ├── svc/                          ← Public Service
//	│   ├── handler/{프로토콜}/
//	│   ├── infra/
//	│   └── alias.go                      ← 외부 진입점
//	│
//	└── saga/{사가이름}/                    ← Saga 파일들
//	    └── saga.go                        ← 여러 도메인의 Public Service를 조합
type DomainPath struct {
	// ── 도메인 관련 필드 ──
	Domain    string // 도메인 이름. 예: "user", "order", "payment"
	Subdomain string // 서브도메인 이름. 예: "core", "role" (서브도메인이 아닌 파일은 빈 문자열)
	Layer     string // 레이어 이름. 예: "model", "repo", "svc", "handler", "infra", "root"
	Protocol  string // 핸들러 프로토콜. 예: "http", "grpc" (handler 레이어에서만 사용)
	File      string // 파일명. 예: "user.go", "alias.go", "saga.go"

	// ── Saga 관련 필드 ──
	IsSaga   bool   // true면 Saga 파일, false면 도메인 파일
	SagaName string // Saga 이름. 예: "create_order", "cancel_order"
}

// ParseDomainPath는 프로젝트 루트 기준 상대 경로를 받아서 DomainPath로 분해한다.
//
// 두 가지 경로 패턴을 인식한다:
//
// 1. 도메인 경로: "internal/domain/{도메인}/..."
//    → Domain, Subdomain, Layer 등이 채워짐
//
// 2. Saga 경로: "internal/saga/{사가이름}/..."
//    → IsSaga=true, SagaName이 채워짐
//
// 어느 패턴에도 해당하지 않으면 nil을 반환한다.
func ParseDomainPath(relPath string) *DomainPath {
	// 경로를 "/" 기준으로 분리한다.
	// filepath.ToSlash는 Windows의 "\"를 "/"로 바꿔준다 (크로스 플랫폼 호환).
	parts := strings.Split(filepath.ToSlash(relPath), "/")

	// "internal" 위치를 찾는다. 모든 경로는 "internal/"로 시작한다.
	internalIdx := -1
	for i, p := range parts {
		if p == "internal" {
			internalIdx = i
			break
		}
	}
	if internalIdx < 0 || internalIdx+1 >= len(parts) {
		return nil
	}

	// "internal" 다음 디렉토리가 "domain"인지 "saga"인지에 따라 분기
	nextDir := parts[internalIdx+1]

	switch nextDir {
	case "saga":
		return parseSagaPath(parts, internalIdx)
	case "domain":
		return parseDomainPathParts(parts, internalIdx)
	default:
		return nil // domain도 saga도 아닌 경로
	}
}

// ──────────────────────────────────────────────
// Saga 경로 파싱
// ──────────────────────────────────────────────

// parseSagaPath는 "internal/saga/{사가이름}/..." 형태의 경로를 파싱한다.
//
// 예: "internal/saga/create_order/saga.go"
// → DomainPath{IsSaga:true, SagaName:"create_order", File:"saga.go"}
func parseSagaPath(parts []string, internalIdx int) *DomainPath {
	// sagaNameIdx: "internal"(i) → "saga"(i+1) → 사가이름(i+2)
	sagaNameIdx := internalIdx + 2
	if sagaNameIdx >= len(parts) {
		return nil
	}

	dp := &DomainPath{
		IsSaga:   true,
		SagaName: parts[sagaNameIdx],
	}

	// 사가 이름 이후의 경로에서 파일명을 추출
	remaining := parts[sagaNameIdx+1:]
	if len(remaining) > 0 {
		last := remaining[len(remaining)-1]
		if strings.HasSuffix(last, ".go") {
			dp.File = last
		}
	}

	return dp
}

// ──────────────────────────────────────────────
// 도메인 경로 파싱
// ──────────────────────────────────────────────

// parseDomainPathParts는 "internal/domain/{도메인}/..." 형태의 경로를 파싱한다.
//
// 예: "internal/domain/user/subdomain/core/model/user.go"
// → DomainPath{Domain:"user", Subdomain:"core", Layer:"model", File:"user.go"}
func parseDomainPathParts(parts []string, internalIdx int) *DomainPath {
	// domainIdx: "internal"(i) → "domain"(i+1) → 도메인이름(i+2)
	domainIdx := internalIdx + 2
	if domainIdx >= len(parts) {
		return nil
	}

	dp := &DomainPath{
		Domain: parts[domainIdx],
	}

	// 도메인 이름 이후의 나머지 경로 부분
	remaining := parts[domainIdx+1:]

	// 마지막 요소가 .go 파일이면 파일명으로 저장하고 remaining에서 제거
	if len(remaining) > 0 {
		last := remaining[len(remaining)-1]
		if strings.HasSuffix(last, ".go") {
			dp.File = last
			remaining = remaining[:len(remaining)-1]
		}
	}

	// 도메인 루트에 있는 파일 (예: alias.go)
	if len(remaining) == 0 {
		dp.Layer = "root"
		return dp
	}

	// 첫 번째 디렉토리에 따라 분류
	switch remaining[0] {
	case "subdomain":
		// subdomain/{서브도메인명}/{레이어}/ 구조
		if len(remaining) >= 2 {
			dp.Subdomain = remaining[1] // 서브도메인 이름 (예: "core", "role")
		}
		if len(remaining) >= 3 {
			dp.Layer = remaining[2] // 레이어 이름 (예: "model", "repo", "svc")
		}
	case "svc":
		// svc/ = 도메인의 공개 서비스 (Public Service)
		dp.Layer = "svc"
	case "handler":
		// handler/{프로토콜}/ 구조 (예: handler/http/)
		dp.Layer = "handler"
		if len(remaining) >= 2 {
			dp.Protocol = remaining[1]
		}
	case "infra":
		// infra/ = 외부 서비스 호출 클라이언트
		dp.Layer = "infra"
	default:
		// 그 외의 디렉토리는 이름 그대로 레이어로 저장
		dp.Layer = remaining[0]
	}

	return dp
}

// ImportToDomainPath는 Go import 경로를 DomainPath로 변환한다.
//
// 도메인 import 예시:
//
//	"rest-api/internal/domain/user/subdomain/core/model"
//	→ moduleName("rest-api")을 제거 → "internal/domain/user/subdomain/core/model"
//	→ ParseDomainPath로 분석 → Domain:"user", Subdomain:"core", Layer:"model"
//
// Saga import 예시:
//
//	"rest-api/internal/saga/create_order"
//	→ moduleName("rest-api")을 제거 → "internal/saga/create_order"
//	→ ParseDomainPath로 분석 → IsSaga:true, SagaName:"create_order"
//
// 외부 패키지(예: "fmt", "net/http")는 nil을 반환한다.
func ImportToDomainPath(importPath, moduleName string) *DomainPath {
	// 우리 프로젝트 모듈의 import인지 확인
	if !strings.HasPrefix(importPath, moduleName+"/") {
		return nil // 외부 패키지이므로 무시
	}

	// 모듈명 부분을 제거해서 프로젝트 내 상대 경로를 얻는다
	relPath := strings.TrimPrefix(importPath, moduleName+"/")
	return ParseDomainPath(relPath)
}
