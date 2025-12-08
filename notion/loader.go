package notion

import (
	"context"
	"fmt"
	"strings"
	"time"

	"goc-notion-reg/models"

	"github.com/jomei/notionapi"
)

const (
	chunkSize      = 1000 // 청킹 크기 (문자 단위)
	rateLimitDelay = 350 * time.Millisecond
)

// Loader Notion API를 사용하여 문서를 로드하는 구조체
type Loader struct {
	client *notionapi.Client
}

// NewLoader 새로운 Notion 로더를 생성합니다
func NewLoader(apiKey string) *Loader {
	return &Loader{
		client: notionapi.NewClient(notionapi.Token(apiKey)),
	}
}

// FetchAllPages 모든 Notion 페이지를 가져와서 Document 슬라이스로 변환합니다
func (l *Loader) FetchAllPages(ctx context.Context) ([]*models.Document, error) {
	var allDocuments []*models.Document

	// Search API로 모든 페이지 조회
	pages, err := l.searchAllPages(ctx)
	if err != nil {
		return nil, fmt.Errorf("페이지 검색 실패: %w", err)
	}

	fmt.Printf("📄 총 %d개의 페이지를 찾았습니다.\n", len(pages))

	// 각 페이지 처리
	for i, page := range pages {
		fmt.Printf("처리 중: %d/%d - %s\n", i+1, len(pages), getPageTitle(page))

		// 페이지 블록 가져오기 (PageID를 BlockID로 변환)
		pageID := string(page.ID)
		content, err := l.fetchPageContent(ctx, notionapi.BlockID(pageID))
		if err != nil {
			fmt.Printf("⚠️  페이지 %s 처리 실패: %v\n", pageID, err)
			continue
		}

		// 페이지 메타데이터 구성
		meta := map[string]string{
			"page_id":   pageID,
			"title":     getPageTitle(page),
			"url":       getPageURL(page),
			"created":   page.CreatedTime.Format(time.RFC3339),
			"last_edit": page.LastEditedTime.Format(time.RFC3339),
		}

		// 콘텐츠 길이 확인 및 디버깅
		contentLen := len([]rune(content))
		fmt.Printf("  콘텐츠 길이: %d자\n", contentLen)

		// 빈 콘텐츠 또는 너무 짧은 콘텐츠는 건너뛰기
		if contentLen < 10 {
			fmt.Printf("  ⚠️  콘텐츠가 너무 짧아 건너뜁니다 (길이: %d자)\n", contentLen)
			if contentLen > 0 {
				fmt.Printf("  콘텐츠 미리보기: %s\n", content[:min(100, len(content))])
			}
			continue
		}

		// 청킹 처리
		chunks := chunkText(content, chunkSize)
		fmt.Printf("  청크 개수: %d개\n", len(chunks))

		for idx, chunk := range chunks {
			chunkLen := len([]rune(chunk))
			doc := &models.Document{
				ID:           fmt.Sprintf("%s-chunk-%d", pageID, idx),
				Title:        getPageTitle(page),
				Content:      chunk,
				ParentPageID: pageID,
				Meta:         meta,
			}
			allDocuments = append(allDocuments, doc)
			fmt.Printf("    청크 %d: %d자 저장\n", idx, chunkLen)
		}

		// Rate limit 방지
		time.Sleep(rateLimitDelay)
	}

	return allDocuments, nil
}

// searchAllPages Search API를 사용하여 모든 페이지를 검색합니다
func (l *Loader) searchAllPages(ctx context.Context) ([]notionapi.Page, error) {
	var allPages []notionapi.Page
	var cursor string

	for {
		req := &notionapi.SearchRequest{
			Filter: notionapi.SearchFilter{
				Value:    "page",
				Property: "object",
			},
		}

		if cursor != "" {
			req.StartCursor = notionapi.Cursor(cursor)
		}

		resp, err := l.client.Search.Do(ctx, req)
		if err != nil {
			return nil, err
		}

		// Object를 Page로 변환
		for _, obj := range resp.Results {
			if obj.GetObject() == notionapi.ObjectTypePage {
				// Page는 포인터 타입으로 Object 인터페이스를 구현
				if pagePtr, ok := obj.(*notionapi.Page); ok {
					allPages = append(allPages, *pagePtr)
				}
			}
		}

		if !resp.HasMore {
			break
		}

		cursor = string(resp.NextCursor)
		time.Sleep(rateLimitDelay)
	}

	return allPages, nil
}

// fetchPageContent 페이지의 모든 블록을 재귀적으로 가져와서 텍스트로 변환합니다
func (l *Loader) fetchPageContent(ctx context.Context, pageID notionapi.BlockID) (string, error) {
	var contentParts []string

	err := l.fetchBlocksRecursive(ctx, pageID, &contentParts, 0)
	if err != nil {
		return "", err
	}

	result := strings.Join(contentParts, "\n\n")

	// 디버깅: 빈 콘텐츠 경고
	if strings.TrimSpace(result) == "" {
		fmt.Printf("  [경고] 페이지 %s의 콘텐츠가 비어있습니다.\n", pageID)
	}

	return result, nil
}

// fetchBlocksRecursive 블록을 재귀적으로 가져와서 텍스트를 추출합니다
func (l *Loader) fetchBlocksRecursive(ctx context.Context, blockID notionapi.BlockID, contentParts *[]string, depth int) error {
	// 최대 깊이 제한 (무한 재귀 방지)
	if depth > 20 {
		return nil
	}

	blocks, err := l.client.Block.GetChildren(ctx, blockID, &notionapi.Pagination{
		PageSize: 100,
	})
	if err != nil {
		return err
	}

	for _, block := range blocks.Results {
		// ChildPageBlock이나 LinkToPageBlock은 다른 페이지를 가리키므로 재귀하지 않음
		switch block.(type) {
		case *notionapi.ChildPageBlock, *notionapi.ChildDatabaseBlock:
			// 하위 페이지나 데이터베이스는 링크만 표시하고 재귀하지 않음
			text := extractTextFromBlock(block, depth)
			if text != "" {
				*contentParts = append(*contentParts, text)
			}
			continue
		}

		text := extractTextFromBlock(block, depth)
		if text != "" {
			*contentParts = append(*contentParts, text)
		} else {
			// 디버깅: 텍스트가 없는 블록 타입 로그 (최상위 레벨만)
			if depth == 0 {
				blockType := fmt.Sprintf("%T", block)
				fmt.Printf("  [경고] 텍스트가 없는 블록: %s (HasChildren: %v)\n", blockType, block.GetHasChildren())
			}
		}

		// 자식 블록이 있으면 재귀 호출 (단, 페이지 링크 블록은 제외)
		if block.GetHasChildren() {
			// 페이지 링크 블록이 아닌 경우에만 재귀
			if _, isChildPage := block.(*notionapi.ChildPageBlock); !isChildPage {
				if _, isChildDB := block.(*notionapi.ChildDatabaseBlock); !isChildDB {
					if err := l.fetchBlocksRecursive(ctx, block.GetID(), contentParts, depth+1); err != nil {
						return err
					}
				}
			}
		}
	}

	time.Sleep(rateLimitDelay)
	return nil
}

// extractTextFromBlock 블록에서 텍스트를 추출합니다
func extractTextFromBlock(block notionapi.Block, depth int) string {
	prefix := strings.Repeat("#", depth+1) + " "

	switch b := block.(type) {
	case *notionapi.ParagraphBlock:
		return prefix + extractRichText(b.Paragraph.RichText)
	case *notionapi.Heading1Block:
		return "# " + extractRichText(b.Heading1.RichText)
	case *notionapi.Heading2Block:
		return "## " + extractRichText(b.Heading2.RichText)
	case *notionapi.Heading3Block:
		return "### " + extractRichText(b.Heading3.RichText)
	case *notionapi.BulletedListItemBlock:
		return "- " + extractRichText(b.BulletedListItem.RichText)
	case *notionapi.NumberedListItemBlock:
		return "1. " + extractRichText(b.NumberedListItem.RichText)
	case *notionapi.ToDoBlock:
		mark := " "
		if b.ToDo.Checked {
			mark = "x"
		}
		return fmt.Sprintf("- [%s] %s", mark, extractRichText(b.ToDo.RichText))
	case *notionapi.CodeBlock:
		return "```\n" + extractRichText(b.Code.RichText) + "\n```"
	case *notionapi.QuoteBlock:
		return "> " + extractRichText(b.Quote.RichText)
	case *notionapi.CalloutBlock:
		return extractRichText(b.Callout.RichText)
	case *notionapi.ToggleBlock:
		// Toggle 블록 처리 (자식 블록은 재귀에서 처리됨)
		return extractRichText(b.Toggle.RichText)
	case *notionapi.ChildPageBlock:
		// 하위 페이지는 제목만 표시
		return fmt.Sprintf("📄 [페이지 링크: %s]", b.ChildPage.Title)
	case *notionapi.ChildDatabaseBlock:
		// 하위 데이터베이스는 제목만 표시
		return fmt.Sprintf("🗄️ [데이터베이스 링크: %s]", b.ChildDatabase.Title)
	case *notionapi.DividerBlock:
		// 구분선은 무시 (의미 있는 콘텐츠가 아님)
		return ""
	case *notionapi.TableBlock:
		// 테이블은 자식 블록(TableRowBlock)에서 처리됨
		return ""
	case *notionapi.TableRowBlock:
		// 테이블 행 처리 (간단하게만)
		if len(b.TableRow.Cells) > 0 {
			var cells []string
			for _, cell := range b.TableRow.Cells {
				cellText := extractRichText(cell)
				if cellText != "" {
					cells = append(cells, cellText)
				}
			}
			if len(cells) > 0 {
				return "| " + strings.Join(cells, " | ") + " |"
			}
		}
		return ""
	case *notionapi.LinkToPageBlock:
		// 다른 페이지로의 링크
		return fmt.Sprintf("🔗 [페이지 링크: %s]", string(b.LinkToPage.PageID))
	case *notionapi.BookmarkBlock:
		// 북마크 블록
		url := b.Bookmark.URL
		caption := extractRichText(b.Bookmark.Caption)
		if caption != "" {
			return fmt.Sprintf("🔖 [북마크: %s](%s)", caption, url)
		}
		return fmt.Sprintf("🔖 [북마크: %s]", url)
	case *notionapi.ImageBlock:
		// 이미지 블록
		caption := extractRichText(b.Image.Caption)
		if caption != "" {
			return fmt.Sprintf("🖼️ [이미지: %s]", caption)
		}
		return "[이미지]"
	case *notionapi.VideoBlock:
		// 비디오 블록
		caption := extractRichText(b.Video.Caption)
		if caption != "" {
			return fmt.Sprintf("🎥 [비디오: %s]", caption)
		}
		return "[비디오]"
	case *notionapi.FileBlock:
		// 파일 블록
		caption := extractRichText(b.File.Caption)
		if caption != "" {
			return fmt.Sprintf("📎 [파일: %s]", caption)
		}
		return "[파일]"
	default:
		// 처리하지 않는 블록 타입 로그 출력 (디버깅용)
		blockType := fmt.Sprintf("%T", block)
		fmt.Printf("  [경고] 처리하지 않는 블록 타입: %s\n", blockType)
		return ""
	}
}

// extractRichText RichText 배열에서 텍스트를 추출합니다
func extractRichText(richText []notionapi.RichText) string {
	var parts []string
	for _, rt := range richText {
		parts = append(parts, rt.PlainText)
	}
	return strings.Join(parts, "")
}

// chunkText 텍스트를 지정된 크기로 청킹합니다
func chunkText(text string, size int) []string {
	if len(text) <= size {
		return []string{text}
	}

	var chunks []string
	runes := []rune(text)

	for i := 0; i < len(runes); i += size {
		end := i + size
		if end > len(runes) {
			end = len(runes)
		}
		chunks = append(chunks, string(runes[i:end]))
	}

	return chunks
}

// min 두 정수 중 작은 값을 반환합니다
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// getPageTitle 페이지에서 제목을 추출합니다
func getPageTitle(page notionapi.Page) string {
	props := page.Properties
	if titleProp, ok := props["title"]; ok {
		if title, ok := titleProp.(*notionapi.TitleProperty); ok {
			return extractRichText(title.Title)
		}
	}

	// Title 속성이 없으면 Name 속성 확인
	if nameProp, ok := props["Name"]; ok {
		if title, ok := nameProp.(*notionapi.TitleProperty); ok {
			return extractRichText(title.Title)
		}
	}

	return "제목 없음"
}

// getPageURL 페이지 URL을 생성합니다
func getPageURL(page notionapi.Page) string {
	return fmt.Sprintf("https://www.notion.so/%s", strings.ReplaceAll(string(page.ID), "-", ""))
}
