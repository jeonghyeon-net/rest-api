package model

import "time"

// Todo는 할 일 엔티티다.
// DDD에서 model 레이어는 비즈니스 규칙의 핵심 데이터 구조를 정의한다.
// NestJS에서 @Entity()로 정의하는 TypeORM 엔티티와 비슷하다.
//
// json 태그는 JSON 직렬화 시 필드명을 지정한다.
// NestJS에서 class-transformer의 @Expose() 데코레이터와 같다.
type Todo struct {
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
	Title     string    `json:"title"`
	Body      string    `json:"body"`
	ID        int64     `json:"id"`
	Done      bool      `json:"done"`
}

// TodoTag는 Todo에 연결된 Tag의 요약 정보다.
// API 응답에서 Todo에 포함되는 Tag 정보를 표현한다.
//
// core 서브도메인에서 tag 서브도메인의 model을 직접 import할 수 없으므로
// (아키텍처 규칙: 서브도메인 간 의존은 core 방향만 허용),
// 필요한 필드만 포함하는 별도 구조체를 정의한다.
type TodoTag struct {
	Name string `json:"name"`
	ID   int64  `json:"id"`
}

// TodoWithTags는 Todo와 연결된 Tag 목록을 함께 반환하는 응답 구조체다.
// Go의 구조체 임베딩(embedding)으로 Todo의 모든 필드를 상속받는다.
// NestJS에서 extends로 클래스를 확장하는 것과 비슷하다.
type TodoWithTags struct {
	Tags []TodoTag `json:"tags"`
	Todo
}

// TodoList는 페이지네이션된 Todo 목록 응답이다.
type TodoList struct {
	Data []TodoWithTags `json:"data"`
	Meta PageMeta       `json:"meta"`
}

// PageMeta는 offset 기반 페이지네이션의 메타데이터다.
// 클라이언트가 전체 페이지 수와 현재 위치를 파악할 수 있게 한다.
type PageMeta struct {
	Page       int   `json:"page"`
	Limit      int   `json:"limit"`
	Total      int64 `json:"total"`
	TotalPages int64 `json:"totalPages"`
}
