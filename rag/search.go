package rag

import (
	"context"
	"fmt"
	"strings"
	"time"

	"goc-notion-rag/db"
	"goc-notion-rag/embedding"
	"goc-notion-rag/models"

	"github.com/google/generative-ai-go/genai"
	"google.golang.org/api/option"
)

// Searcher RAG 검색을 수행하는 구조체
type Searcher struct {
	embedder    *embedding.Embedder
	store       *db.Store
	genaiClient *genai.Client
	model       *genai.GenerativeModel
	ctx         context.Context
}

// NewSearcher 새로운 RAG 검색기를 생성합니다
func NewSearcher(ctx context.Context, geminiAPIKey string, store *db.Store) (*Searcher, error) {
	// 임베딩 생성기 초기화
	embedder, err := embedding.NewEmbedder(ctx, geminiAPIKey)
	if err != nil {
		return nil, fmt.Errorf("임베딩 생성기 초기화 실패: %w", err)
	}

	// Gemini 클라이언트 초기화 (생성용)
	genaiClient, err := genai.NewClient(ctx, option.WithAPIKey(geminiAPIKey))
	if err != nil {
		embedder.Close()
		return nil, fmt.Errorf("Gemini 클라이언트 생성 실패: %w", err)
	}

	model := genaiClient.GenerativeModel("gemini-2.5-flash")

	return &Searcher{
		embedder:    embedder,
		store:       store,
		genaiClient: genaiClient,
		model:       model,
		ctx:         ctx,
	}, nil
}

// Search 질문에 대한 RAG 검색을 수행하고 답변을 반환합니다
func (s *Searcher) Search(question string) (string, error) {
	// 1. 질문을 임베딩으로 변환 (검색 시 RETRIEVAL_QUERY 사용)
	queryVector, err := s.embedder.EmbedText(question, "RETRIEVAL_QUERY")
	if err != nil {
		return "", fmt.Errorf("질문 임베딩 실패: %w", err)
	}

	// 2. 벡터 DB에서 Top 10 검색 (더 많은 결과를 가져와서 관련 문서를 놓치지 않도록)
	documents, err := s.store.Search(s.ctx, queryVector, 10)
	if err != nil {
		return "", fmt.Errorf("문서 검색 실패: %w", err)
	}

	if len(documents) == 0 {
		return "유사도 0.7 이상인 관련 문서를 찾을 수 없습니다.", nil
	}

	// 3. 검색된 문서들을 컨텍스트로 구성
	contextText := s.buildContext(documents)

	// 4. 프롬프트 구성
	prompt := s.buildPrompt(contextText, question)

	// 5. Gemini에 질문 전송
	answer, err := s.generateAnswer(prompt)
	if err != nil {
		return "", fmt.Errorf("답변 생성 실패: %w", err)
	}

	return answer, nil
}

// buildContext 검색된 문서들을 컨텍스트 텍스트로 구성합니다
func (s *Searcher) buildContext(documents []*models.Document) string {
	var parts []string

	for i, doc := range documents {
		title := doc.Title
		if title == "" {
			title = "제목 없음"
		}

		parts = append(parts, fmt.Sprintf("[문서 %d: %s]\n%s", i+1, title, doc.Content))
	}

	return strings.Join(parts, "\n\n---\n\n")
}

// buildPrompt 컨텍스트와 질문을 포함한 프롬프트를 구성합니다
func (s *Searcher) buildPrompt(contextText, question string) string {
	return fmt.Sprintf(`당신은 나의 Notion 개인 비서입니다. 아래 [Context]를 바탕으로 질문에 답하세요.
모르는 내용은 지어내지 말고 모른다고 하세요.

[Context]
%s

[Question]
%s

답변:`, contextText, question)
}

// generateAnswer Gemini API를 사용하여 답변을 생성합니다
// Rate Limit 에러 발생 시 30초 대기 후 재시도합니다
func (s *Searcher) generateAnswer(prompt string) (string, error) {
	const maxRetries = 3
	const retryDelay = 30 * time.Second

	var lastErr error
	for attempt := 0; attempt < maxRetries; attempt++ {
		resp, err := s.model.GenerateContent(s.ctx, genai.Text(prompt))
		if err == nil {
			// 성공 시 응답 처리
			var answerParts []string
			for _, cand := range resp.Candidates {
				if cand.Content != nil {
					for _, part := range cand.Content.Parts {
						if text, ok := part.(genai.Text); ok {
							answerParts = append(answerParts, string(text))
						}
					}
				}
			}

			if len(answerParts) == 0 {
				return "답변을 생성할 수 없습니다.", nil
			}

			return strings.Join(answerParts, "\n"), nil
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
		return "", err
	}

	return "", fmt.Errorf("최대 재시도 횟수 초과: %w", lastErr)

}

// Close 리소스를 정리합니다
func (s *Searcher) Close() error {
	var errs []error

	if s.embedder != nil {
		if err := s.embedder.Close(); err != nil {
			errs = append(errs, err)
		}
	}

	if s.genaiClient != nil {
		if err := s.genaiClient.Close(); err != nil {
			errs = append(errs, err)
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("리소스 정리 중 오류 발생: %v", errs)
	}

	return nil
}

