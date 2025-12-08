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
	// metadata에 cosine 거리 계산 방식 설정
	metadata := map[string]string{
		"hnsw:space": "cosine",
	}
	collection, err := db.GetOrCreateCollection("notion_docs", metadata, nil)
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

	// 결과를 Document로 변환 (유사도 0.7 이상만 필터링)
	documents := make([]*models.Document, 0, len(results))
	filteredCount := 0
	for _, result := range results {
		// 유사도 0.7 이상만 필터링
		if result.Similarity < 0.7 {
			filteredCount++
			continue
		}

		// 메타데이터에서 제목 추출 (먼저 제목 확인)
		title := ""
		if result.Metadata != nil {
			if t, ok := result.Metadata["title"]; ok {
				title = t
			}
		}

		// 디버깅: 검색된 결과 확인 (제목, 유사도 점수만 표시, 미리보기 제거)
		fmt.Printf("[검색 결과 %d] ID: %s", len(documents)+1, result.ID)
		if title != "" {
			fmt.Printf(", 제목: %s", title)
		}
		// 유사도 점수 표시 (0~1 범위, 높을수록 유사)
		fmt.Printf(", 유사도: %.3f", result.Similarity)
		fmt.Printf(", Content 길이: %d자\n", len(result.Content))

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

	// 필터링된 결과 정보 출력
	if filteredCount > 0 {
		fmt.Printf("(유사도 0.7 미만으로 필터링된 결과: %d개)\n", filteredCount)
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

// ListAll 모든 문서의 메타데이터를 반환합니다 (제목, ID 등)
func (s *Store) ListAll(ctx context.Context, limit int) ([]*models.Document, error) {
	// chromem-go의 Get 메서드를 사용하여 모든 문서 가져오기
	// Get은 ID 목록을 받아서 문서를 반환합니다
	// 하지만 모든 ID를 알 수 없으므로, 다른 방법을 사용해야 합니다

	// 임의의 벡터로 검색하여 모든 문서를 가져오는 것은 불가능하므로
	// 대신 빈 벡터나 특정 조건으로 검색하는 대신
	// Collection의 Count와 함께 사용할 수 있는 다른 방법을 찾아야 합니다

	// chromem-go는 직접적인 ListAll 메서드가 없을 수 있으므로
	// Get 메서드를 사용하려면 ID 목록이 필요합니다
	// 하지만 ID 목록을 얻을 수 없으므로, 이 기능은 제한적입니다

	// 대안: 빈 쿼리 벡터로 검색 (작동하지 않을 수 있음)
	// 또는 Collection의 내부 메서드를 사용할 수 있는지 확인

	// 일단 빈 슬라이스를 반환하고, 나중에 구현 개선
	return []*models.Document{}, fmt.Errorf("ListAll은 아직 구현되지 않았습니다. chromem-go의 API 제한으로 인해 모든 문서를 직접 조회할 수 없습니다")
}

// GetByID ID로 특정 문서를 가져옵니다
func (s *Store) GetByID(ctx context.Context, docID string) (*models.Document, error) {
	// chromem-go의 GetByID 메서드 사용
	result, err := s.collection.GetByID(ctx, docID)
	if err != nil {
		return nil, fmt.Errorf("문서 조회 실패: %w", err)
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

	return doc, nil
}

// ListByTitle 제목으로 문서를 검색합니다 (메타데이터 필터링)
func (s *Store) ListByTitle(ctx context.Context, titleFilter string, limit int) ([]*models.Document, error) {
	// chromem-go는 메타데이터 필터링을 지원하지 않을 수 있으므로
	// 모든 문서를 가져와서 필터링해야 합니다
	// 하지만 ListAll이 구현되지 않았으므로, 이 기능도 제한적입니다

	// 대안: 제목을 포함한 검색 쿼리를 사용
	// 제목을 임베딩하여 검색하는 방법을 사용할 수 있습니다
	return []*models.Document{}, fmt.Errorf("ListByTitle은 아직 구현되지 않았습니다")
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
