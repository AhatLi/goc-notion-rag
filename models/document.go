package models

// Document Notion에서 가져온 문서를 나타내는 구조체
type Document struct {
	ID           string            // 문서 ID (청크의 경우 고유 ID)
	Title        string            // 페이지 제목
	Content      string            // 본문 텍스트
	Vector       []float32         // 임베딩 벡터
	Meta         map[string]string // 메타데이터 (URL, 작성일 등)
	ParentPageID string            // 원본 페이지 ID (청킹된 경우)
}
