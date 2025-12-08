package ui

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"goc-notion-reg/rag"
)

// Run 간단한 REPL 스타일의 검색 인터페이스를 실행합니다
func Run(searcher *rag.Searcher) error {
	scanner := bufio.NewScanner(os.Stdin)

	fmt.Println("📚 Notion RAG 검색")
	fmt.Println("질문을 입력하세요 (종료: 'exit' 또는 'q', Ctrl+C)")
	fmt.Println()

	for {
		fmt.Print("> ")
		if !scanner.Scan() {
			break
		}

		question := strings.TrimSpace(scanner.Text())
		if question == "" {
			continue
		}

		if question == "exit" || question == "q" {
			fmt.Println("\n👋 안녕히 가세요!")
			break
		}

		// 검색 실행
		fmt.Println("🔍 검색 중...")
		answer, err := searcher.Search(question)
		if err != nil {
			fmt.Printf("❌ 오류: %v\n\n", err)
			continue
		}

		// 답변 표시
		fmt.Println("\n💬 답변:")
		fmt.Println(answer)
		fmt.Println()
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("입력 읽기 오류: %w", err)
	}

	return nil
}
