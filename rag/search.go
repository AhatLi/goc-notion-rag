package rag

import (
	"context"
	"fmt"
	"strings"

	"goc-notion-reg/db"
	"goc-notion-reg/embedding"
	"goc-notion-reg/models"

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
	// 1. 질문을 임베딩으로 변환
	queryVector, err := s.embedder.EmbedText(question)
	if err != nil {
		return "", fmt.Errorf("질문 임베딩 실패: %w", err)
	}

	// 2. 벡터 DB에서 Top 5 검색
	documents, err := s.store.Search(s.ctx, queryVector, 5)
	if err != nil {
		return "", fmt.Errorf("문서 검색 실패: %w", err)
	}

	if len(documents) == 0 {
		return "관련 문서를 찾을 수 없습니다.", nil
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
func (s *Searcher) generateAnswer(prompt string) (string, error) {
	resp, err := s.model.GenerateContent(s.ctx, genai.Text(prompt))
	if err != nil {
		return "", err
	}

	// 응답에서 텍스트 추출
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
