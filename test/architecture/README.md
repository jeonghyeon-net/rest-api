# Architecture Test

`go/ast`를 사용한 아키텍처 규칙 자동 검증 시스템.

> 참고: [채널톡 블로그 - AI가 규칙을 지키는 백엔드 리팩토링](https://channel.io/ko/team/blog/articles/ai-native-ddd-refactoring-98c23cdb)

## 실행 방법

```bash
# 전체 아키텍처 테스트 실행
go test ./test/architecture/... -v

# 캐시 무시하고 실행 (-count=1)
go test ./test/architecture/... -v -count=1

# 특정 규칙만 실행
go test ./test/architecture/... -v -run TestArchitecture/Dependencies
go test ./test/architecture/... -v -run TestArchitecture/Naming
go test ./test/architecture/... -v -run TestArchitecture/InterfacePatterns
go test ./test/architecture/... -v -run TestArchitecture/Structure
```

## 이상적인 디렉토리 구조

```
rest-api/
├── main.go                              # 앱 진입점
├── go.mod
├── go.sum
│
├── internal/                            # 외부 패키지에서 import 불가 (Go 컨벤션)
│   │
│   ├── domain/                          # ── 도메인 영역 ──
│   │   │
│   │   ├── user/                        # user 도메인
│   │   │   ├── alias.go                 # 외부 공개용 타입 별칭 (유일한 진입점)
│   │   │   │                            #   예: type Public = svc.Public
│   │   │   │
│   │   │   ├── subdomain/               # 서브도메인들
│   │   │   │   ├── core/                #   핵심 서브도메인 (다른 서브도메인이 의존 가능)
│   │   │   │   │   ├── model/           #     엔티티, 값 객체
│   │   │   │   │   │   └── user.go      #       type User struct { ID, Name, Email }
│   │   │   │   │   ├── repo/            #     저장소 인터페이스
│   │   │   │   │   │   └── user.go      #       type User interface { FindByID, Save }
│   │   │   │   │   └── svc/             #     비즈니스 로직
│   │   │   │   │       └── user.go      #       type User interface { GetUser, CreateUser }
│   │   │   │   │
│   │   │   │   └── role/                #   역할 서브도메인 (core에만 의존 가능)
│   │   │   │       ├── model/
│   │   │   │       ├── repo/
│   │   │   │       └── svc/
│   │   │   │
│   │   │   ├── svc/                     # 공개 서비스 인터페이스 (Public Service)
│   │   │   │   └── public.go            #   alias.go를 통해서만 외부에 노출
│   │   │   │
│   │   │   ├── handler/                 # 요청 핸들러 (어댑터 레이어)
│   │   │   │   ├── http/                #   REST API 핸들러
│   │   │   │   │   └── handler.go
│   │   │   │   └── grpc/                #   gRPC 핸들러
│   │   │   │       └── handler.go
│   │   │   │
│   │   │   └── infra/                   # 외부 서비스 클라이언트
│   │   │       └── email/
│   │   │           └── client.go
│   │   │
│   │   ├── order/                       # order 도메인 (동일 구조)
│   │   │   ├── alias.go
│   │   │   ├── subdomain/
│   │   │   ├── svc/
│   │   │   ├── handler/
│   │   │   └── infra/
│   │   │
│   │   └── payment/                     # payment 도메인 (동일 구조)
│   │       └── ...
│   │
│   └── saga/                            # ── Saga 영역 ──
│       │                                # 여러 도메인에 걸친 트랜잭션 조율
│       │
│       ├── create_order/                # 주문 생성 Saga
│       │   └── saga.go                  #   user + order + payment 조합
│       │                                #   각 도메인의 alias.go만 import 가능
│       │
│       └── cancel_order/                # 주문 취소 Saga
│           └── saga.go                  #   보상 트랜잭션(compensation) 처리
│
└── test/
    └── architecture/                    # 아키텍처 규칙 자동 검증 테스트
        ├── arch_test.go                 # 메인 테스트: 4개 규칙 카테고리 실행
        ├── analyzer/
        │   ├── parser.go               # go/ast로 Go 파일 파싱 (import, type, func 추출)
        │   └── domain.go               # 파일 경로 → 도메인/Saga 식별
        ├── report/
        │   └── violation.go            # 규칙 위반 보고서 (Violation 구조체)
        └── ruleset/
            ├── config.go               # 프로젝트 설정 + 허용 목록 상수
            ├── dependency.go           # 의존성 규칙 (8가지: 도메인 5 + Saga 3)
            ├── naming.go               # 네이밍 규칙 (5가지)
            ├── interface_pattern.go    # 인터페이스 패턴 규칙 (3가지)
            └── structure.go            # 디렉토리 구조 규칙 (7가지: 도메인 5 + Saga 2)
```

## 의존성 규칙 매트릭스

### 전체 의존성 매트릭스

| From \ To | 같은 서브도메인 | 다른 서브도메인 | alias.go | 다른 도메인 내부 | Saga |
|-----------|:---:|:---:|:---:|:---:|:---:|
| **subdomain** | O | X (core 제외) | O | X | X |
| **svc (Public)** | O | O | O (다른 도메인) | X | O |
| **handler** | - | - | O | O (alias.go만) | O |
| **saga** | X | X | O (모든 도메인) | X | X |

### 레이어 간 의존 방향 (같은 서브도메인 내)

```
model ← repo ← svc

model: 아무것도 import하지 않음 (가장 안쪽)
repo:  model만 import 가능
svc:   model, repo 모두 import 가능 (가장 바깥)
```

### 서브도메인 간 의존 규칙 (같은 도메인 내)

```
core:  다른 서브도메인 import 불가 (독립적)
role:  core만 import 가능
기타:  core만 import 가능
```

### Saga 의존 규칙

```
Saga → 도메인 alias.go (Public 타입)    ✓  (유일한 진입점)
Saga → 도메인 svc/ (Public Service)     ✗  (alias.go를 통해서만 접근)
Saga → 도메인 subdomain/ (내부)         ✗
Saga → 다른 Saga                       ✗  (각 Saga는 독립적)
subdomain → Saga                       ✗  (서브도메인은 Saga를 모름)
handler → Saga                         ✓  (핸들러에서 Saga 트리거 가능)
```

## 검사 규칙 목록

| 카테고리 | 규칙 | 심각도 | 설명 |
|---------|------|:---:|------|
| **dependency** | cross-subdomain | ERROR | 서브도메인 간 직접 import 금지 (core 예외) |
| | subdomain-imports-domain-layer | ERROR | 서브도메인에서 도메인 레이어(svc/, handler/, infra/) import 금지 |
| | cross-domain | ERROR | 다른 도메인은 alias.go를 통해서만 접근 (svc/ 직접 import 금지) |
| | cross-domain-from-subdomain | ERROR | 서브도메인에서 다른 도메인 직접 import 금지 |
| | layer-direction | ERROR | 레이어 역방향 의존 금지 (model → svc 등) |
| | saga-internal-import | ERROR | Saga는 도메인 alias.go만 import 가능 (내부 패키지 금지) |
| | saga-cross-saga | ERROR | Saga 간 직접 import 금지 |
| | subdomain-imports-saga | ERROR | 서브도메인에서 Saga import 금지 |
| **naming** | forbidden-package | ERROR | util, common, shared, lib 등 금지된 패키지명 |
| | package-stutter | WARNING | 타입명에 패키지명 반복 (repo.AppRepo) |
| | impl-suffix | ERROR | Impl 접미사 금지 |
| | file-interface-match | WARNING | svc/, repo/ 파일명과 인터페이스명 불일치 |
| | layer-suffix-filename | ERROR | 파일명에 레이어 접미사 포함 (install_svc.go) |
| **interface** | missing-impl | WARNING | 공개 인터페이스에 비공개 구현체 없음 |
| | constructor-return | ERROR | 생성자가 인터페이스 대신 구체 타입 반환 |
| | exported-impl | WARNING | 구현체 struct가 공개(exported)됨 |
| **structure** | missing-alias | WARNING | 도메인에 alias.go 없음 |
| | invalid-domain-dir | ERROR | 허용되지 않은 도메인 하위 디렉토리 |
| | invalid-subdomain-layer | ERROR | 허용되지 않은 서브도메인 레이어 |
| | invalid-handler-protocol | ERROR | 허용되지 않은 핸들러 프로토콜 |
| | file-in-subdomain-root | ERROR | subdomain/ 루트에 파일 존재 |
| | file-in-saga-root | ERROR | saga/ 루트에 파일 존재 |
| | saga-nested-dir | ERROR | Saga 안에 하위 디렉토리 존재 |

## NestJS와의 비교

NestJS에 익숙하다면 이렇게 대응시켜 이해할 수 있다:

| NestJS | Go DDD |
|--------|--------|
| Module | domain 디렉토리 (예: `internal/domain/user/`) |
| `exports` in Module | `alias.go` (외부에 공개할 타입 명시) |
| Controller | `handler/http/` (HTTP 요청 처리) |
| Service (Injectable) | `subdomain/core/svc/` (비즈니스 로직) |
| Entity | `subdomain/core/model/` (도메인 모델) |
| Repository | `subdomain/core/repo/` (데이터 접근 인터페이스) |
| Interface + @Injectable | 공개 interface + 비공개 struct + New() 생성자 |
| `useClass` / Provider | `func New() Interface { return &impl{} }` |
| Orchestrator / Use Case | `internal/saga/{이름}/` (여러 도메인 조합) |

## Saga 패턴 설명

Saga는 여러 도메인에 걸친 비즈니스 플로우를 조율한다.

### 예: 주문 생성 Saga

```
[Handler] → [CreateOrder Saga] → [user.Public]   : 사용자 검증
                                → [order.Public]  : 주문 생성
                                → [payment.Public] : 결제 처리
                                → [inventory.Public] : 재고 차감
```

하나의 단계가 실패하면 이전 단계들을 보상 트랜잭션으로 롤백한다:

```
결제 실패 시:
  → [order.Public] : 주문 취소 (보상)
```

### Saga가 도메인과 다른 점

| | Domain | Saga |
|---|---|---|
| 위치 | `internal/domain/{이름}/` | `internal/saga/{이름}/` |
| 역할 | 하나의 비즈니스 영역 | 여러 도메인의 조합 |
| 구조 | subdomain, svc, handler 등 | 단일 패키지 (flat) |
| 의존 | 같은 도메인 내부 + alias.go | 모든 도메인의 alias.go만 |
