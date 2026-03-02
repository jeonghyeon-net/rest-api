# 프로젝트 규칙

## 주석 정책
- 모든 코드에 한국어로 상세한 주석을 남긴다. Go 초심자(Node.js/TypeScript/NestJS 배경)가 이해할 수 있는 수준으로 작성한다.
- Go 특유의 문법이나 패턴(타입 단언, 타입 스위치, 리시버, defer 등)은 반드시 설명한다.
- NestJS의 대응 개념이 있으면 비유해서 설명한다.
- 코드를 수정할 때, 같은 파일 또는 다른 파일에 있는 관련 주석이 현재 코드와 맞지 않게 되었는지 확인하고, outdated된 주석은 함께 업데이트한다.

## 빌드/린트 정책
- `go build`, `golangci-lint run`, `gofmt`, `nilaway` 등 빌드·린트·포맷 도구를 직접 호출하지 않는다. 반드시 Makefile 타겟을 사용한다.
  - 빌드: `make build`
  - 포맷: `make fmt`
  - 린트: `make lint`
  - 아키텍처 검증: `make arch`
  - 개발 서버: `make dev`
- Makefile에 정의되지 않은 명령은 직접 호출해도 된다.
