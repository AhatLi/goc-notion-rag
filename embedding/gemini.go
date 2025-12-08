package embedding

import (
	"context"
	"fmt"
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

	model := client.EmbeddingModel("text-embedding-004")

	return &Embedder{
		client: client,
		model:  model,
		ctx:    ctx,
	}, nil
}

// EmbedText 텍스트를 임베딩 벡터로 변환합니다
func (e *Embedder) EmbedText(text string) ([]float32, error) {
	resp, err := e.model.EmbedContent(e.ctx, genai.Text(text))
	if err != nil {
		return nil, fmt.Errorf("임베딩 생성 실패: %w", err)
	}

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

// EmbedTexts 여러 텍스트를 배치로 임베딩합니다 (Rate limit 방지)
func (e *Embedder) EmbedTexts(texts []string) ([][]float32, error) {
	var results [][]float32

	for i, text := range texts {
		embedding, err := e.EmbedText(text)
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
