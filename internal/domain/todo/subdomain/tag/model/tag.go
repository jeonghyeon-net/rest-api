package model

import "time"

// Tag는 Todo에 부착할 수 있는 분류 라벨이다.
// GitHub의 Label, Jira의 Label과 같은 개념이다.
//
// name은 DB에서 UNIQUE 제약이 걸려 있어 중복 태그명을 생성할 수 없다.
type Tag struct {
	CreatedAt time.Time `json:"createdAt"`
	Name      string    `json:"name"`
	ID        int64     `json:"id"`
}
