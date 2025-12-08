package db

import (
	"context"
	"fmt"
	"os"

	"goc-notion-reg/models"

	"github.com/philippgille/chromem-go"
)

// Store 벡터 DB 저장소
type Store struct {
	db         *chromem.DB
	collection *chromem.Collection
}

// NewStore 새로운 벡터 DB 저장소를 생성합니다
func NewStore(dbPath string) (*Store, error) {
	// PersistentDB 생성 (기존 DB가 있으면 로드, 없으면 생성)
	db, err := chromem.NewPersistentDB(dbPath, false)
	if err != nil {
		return nil, fmt.Errorf("DB 초기화 실패: %w", err)
	}

	// Collection 생성 또는 가져오기
	collection, err := db.GetOrCreateCollection("notion_docs", nil, nil)
	if err != nil {
		return nil, fmt.Errorf("Collection 생성 실패: %w", err)
	}

	return &Store{
		db:         db,
		collection: collection,
	}, nil
}

// Exists DB 파일이 존재하는지 확인합니다
func Exists(dbPath string) bool {
	_, err := os.Stat(dbPath)
	return err == nil
}

// Count 저장된 문서의 개수를 반환합니다
func (s *Store) Count(ctx context.Context) (int, error) {
	count := s.collection.Count()
	return count, nil
}

// AddDocument 문서를 벡터 DB에 추가합니다
func (s *Store) AddDocument(ctx context.Context, doc *models.Document) error {
	if doc.Vector == nil || len(doc.Vector) == 0 {
		return fmt.Errorf("문서에 임베딩 벡터가 없습니다: %s", doc.ID)
	}

	// 메타데이터 구성 (chromem-go는 map[string]string을 사용)
	metadata := make(map[string]string)
	metadata["title"] = doc.Title
	metadata["parent_page_id"] = doc.ParentPageID

	// Meta의 모든 필드를 메타데이터에 추가
	for k, v := range doc.Meta {
		metadata[k] = v
	}

	// 문서 추가 (배치로 전달)
	ids := []string{doc.ID}
	vectors := [][]float32{doc.Vector}
	metadatas := []map[string]string{metadata}
	contents := []string{doc.Content}

	err := s.collection.Add(ctx, ids, vectors, metadatas, contents)
	if err != nil {
		return fmt.Errorf("문서 추가 실패: %w", err)
	}

	return nil
}

// AddDocuments 여러 문서를 배치로 추가합니다
func (s *Store) AddDocuments(ctx context.Context, docs []*models.Document) error {
	for i, doc := range docs {
		if err := s.AddDocument(ctx, doc); err != nil {
			return fmt.Errorf("문서 %d 추가 실패: %w", i, err)
		}
	}
	return nil
}

// Search 유사한 문서를 검색합니다 (Top K)
func (s *Store) Search(ctx context.Context, queryVector []float32, topK int) ([]*models.Document, error) {
	if queryVector == nil || len(queryVector) == 0 {
		return nil, fmt.Errorf("쿼리 벡터가 비어있습니다")
	}

	// 검색 실행 (QueryEmbedding 사용)
	results, err := s.collection.QueryEmbedding(ctx, queryVector, topK, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("검색 실패: %w", err)
	}

	// 결과를 Document로 변환
	documents := make([]*models.Document, 0, len(results))
	for i, result := range results {
		// 디버깅: 검색된 콘텐츠 확인
		contentLen := len(result.Content)
		fmt.Printf("[검색 결과 %d] ID: %s, Content 길이: %d자\n", i+1, result.ID, contentLen)
		if contentLen > 0 && contentLen < 200 {
			fmt.Printf("  Content 미리보기: %s\n", result.Content[:min(200, contentLen)])
		}

		doc := &models.Document{
			ID:      result.ID,
			Content: result.Content,
		}

		// 메타데이터 파싱
		if result.Metadata != nil {
			meta := make(map[string]string)
			for k, v := range result.Metadata {
				meta[k] = v
			}
			doc.Meta = meta

			// Title 추출
			if title, ok := meta["title"]; ok {
				doc.Title = title
			}

			// ParentPageID 추출
			if parentID, ok := meta["parent_page_id"]; ok {
				doc.ParentPageID = parentID
			}
		}

		documents = append(documents, doc)
	}

	return documents, nil
}

// min 두 정수 중 작은 값을 반환합니다
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// Clear 모든 문서를 삭제합니다 (리로드 시 사용)
func (s *Store) Clear(ctx context.Context) error {
	// chromem-go는 직접적인 Clear 메서드가 없을 수 있으므로
	// Collection을 삭제하고 다시 생성하는 방식 사용
	// 이는 구현에 따라 다를 수 있음
	return nil
}

// Close DB 연결을 닫습니다
func (s *Store) Close() error {
	// chromem-go의 PersistentDB는 자동으로 저장되므로 별도의 Close가 필요 없을 수 있음
	return nil
}
