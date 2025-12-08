package embedding

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/generative-ai-go/genai"
	"google.golang.org/api/option"
)

// Embedder Gemini API를 사용하여 텍스트를 임베딩으로 변환하는 구조체
type Embedder struct {
	client *genai.Client
	model  *genai.EmbeddingModel
	ctx    context.Context
}

// NewEmbedder 새로운 임베딩 생성기를 생성합니다
func NewEmbedder(ctx context.Context, apiKey string) (*Embedder, error) {
	client, err := genai.NewClient(ctx, option.WithAPIKey(apiKey))
	if err != nil {
		return nil, fmt.Errorf("Gemini 클라이언트 생성 실패: %w", err)
	}

	model := client.EmbeddingModel("gemini-embedding-001")

	return &Embedder{
		client: client,
		model:  model,
		ctx:    ctx,
	}, nil
}

// EmbedText 텍스트를 임베딩 벡터로 변환합니다
// taskType: "RETRIEVAL_DOCUMENT" (저장 시) 또는 "RETRIEVAL_QUERY" (검색 시)
// Rate Limit 에러 발생 시 30초 대기 후 재시도합니다
func (e *Embedder) EmbedText(text string, taskType string) ([]float32, error) {
	const maxRetries = 3
	const retryDelay = 30 * time.Second

	// TaskType 상수 변환
	var taskTypeEnum genai.TaskType
	switch taskType {
	case "RETRIEVAL_DOCUMENT":
		taskTypeEnum = genai.TaskTypeRetrievalDocument
	case "RETRIEVAL_QUERY":
		taskTypeEnum = genai.TaskTypeRetrievalQuery
	default:
		taskTypeEnum = genai.TaskTypeUnspecified
	}

	// 기존 TaskType 저장
	originalTaskType := e.model.TaskType
	// TaskType 설정
	e.model.TaskType = taskTypeEnum
	defer func() {
		// 원래 TaskType 복원
		e.model.TaskType = originalTaskType
	}()

	var lastErr error
	for attempt := 0; attempt < maxRetries; attempt++ {
		// EmbedContent 호출
		resp, err := e.model.EmbedContent(e.ctx, genai.Text(text))
		if err == nil {
			// 성공 시 응답 처리
			if resp.Embedding == nil {
				return nil, fmt.Errorf("임베딩 응답이 비어있습니다")
			}

			// float64를 float32로 변환
			values := resp.Embedding.Values
			result := make([]float32, len(values))
			for i, v := range values {
				result[i] = float32(v)
			}

			return result, nil
		}

		lastErr = err
		errStr := err.Error()

		// Rate Limit 에러 확인 (429 또는 rate limit 관련 메시지)
		isRateLimit := strings.Contains(errStr, "429") ||
			strings.Contains(strings.ToLower(errStr), "rate limit") ||
			strings.Contains(strings.ToLower(errStr), "quota") ||
			strings.Contains(strings.ToLower(errStr), "resource exhausted")

		if isRateLimit && attempt < maxRetries-1 {
			fmt.Printf("⚠️  Rate Limit 에러 발생 (시도 %d/%d), %v 후 재시도...\n", attempt+1, maxRetries, retryDelay)
			time.Sleep(retryDelay)
			continue
		}

		// Rate Limit이 아니거나 최대 재시도 횟수에 도달한 경우
		return nil, fmt.Errorf("임베딩 생성 실패: %w", err)
	}

	return nil, fmt.Errorf("최대 재시도 횟수 초과: %w", lastErr)
}

// EmbedTexts 여러 텍스트를 배치로 임베딩합니다 (Rate limit 방지)
// taskType: "RETRIEVAL_DOCUMENT" (저장 시) 또는 "RETRIEVAL_QUERY" (검색 시)
func (e *Embedder) EmbedTexts(texts []string, taskType string) ([][]float32, error) {
	var results [][]float32

	for i, text := range texts {
		embedding, err := e.EmbedText(text, taskType)
		if err != nil {
			return nil, fmt.Errorf("텍스트 %d 임베딩 실패: %w", i, err)
		}
		results = append(results, embedding)

		// Rate limit 방지를 위한 짧은 대기
		if i < len(texts)-1 {
			time.Sleep(100 * time.Millisecond)
		}
	}

	return results, nil
}

// Close 클라이언트를 닫습니다
func (e *Embedder) Close() error {
	return e.client.Close()
}
