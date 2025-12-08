package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"

	"goc-notion-reg/db"
	"goc-notion-reg/embedding"
	"goc-notion-reg/notion"
	"goc-notion-reg/rag"
	"goc-notion-reg/ui"
)

func main() {
	// 플래그 파싱
	reload := flag.Bool("reload", false, "Notion 데이터를 새로 가져옵니다")
	flag.Parse()

	ctx := context.Background()

	// 설정 로드
	config, err := LoadConfig()
	if err != nil {
		log.Fatalf("설정 로드 실패: %v", err)
	}

	// DB 초기화
	store, err := db.NewStore(config.DBPath)
	if err != nil {
		log.Fatalf("DB 초기화 실패: %v", err)
	}
	defer store.Close()

	// DB 존재 여부 및 문서 개수 확인
	dbExists := db.Exists(config.DBPath)
	count, _ := store.Count(ctx)

	// 리로드 모드 또는 DB가 비어있는 경우
	if *reload || !dbExists || count == 0 {
		if !*reload && (!dbExists || count == 0) {
			fmt.Println("⚠️  DB가 없거나 비어있습니다. --reload 옵션으로 데이터를 생성해주세요.")
			fmt.Println("   또는 --reload 플래그를 사용하여 자동으로 데이터를 가져옵니다.")
			os.Exit(1)
		}

		fmt.Println("🔄 Notion에서 데이터를 가져오는 중...")

		// Notion 로더 초기화
		loader := notion.NewLoader(config.NotionAPIKey)

		// 모든 페이지 가져오기
		documents, err := loader.FetchAllPages(ctx)
		if err != nil {
			log.Fatalf("Notion 페이지 가져오기 실패: %v", err)
		}

		if len(documents) == 0 {
			log.Fatal("가져온 페이지가 없습니다.")
		}

		fmt.Printf("📄 총 %d개의 문서 청크를 가져왔습니다.\n", len(documents))
		fmt.Println("🧠 임베딩 생성 중 (Gemini)...")

		// 임베딩 생성기 초기화
		embedder, err := embedding.NewEmbedder(ctx, config.GeminiAPIKey)
		if err != nil {
			log.Fatalf("임베딩 생성기 초기화 실패: %v", err)
		}
		defer embedder.Close()

		// 각 문서에 임베딩 생성 및 저장
		for i, doc := range documents {
			contentLen := len([]rune(doc.Content))
			fmt.Printf("임베딩 생성 중: %d/%d - %s (콘텐츠: %d자)\n", i+1, len(documents), doc.Title, contentLen)

			// 콘텐츠가 너무 짧으면 건너뛰기
			if contentLen < 10 {
				fmt.Printf("  ⚠️  콘텐츠가 너무 짧아 건너뜁니다\n")
				continue
			}

			// 임베딩 생성
			vector, err := embedder.EmbedText(doc.Content)
			if err != nil {
				log.Printf("⚠️  문서 %s 임베딩 실패: %v", doc.ID, err)
				continue
			}

			doc.Vector = vector

			// DB에 저장
			if err := store.AddDocument(ctx, doc); err != nil {
				log.Printf("⚠️  문서 %s 저장 실패: %v", doc.ID, err)
				continue
			}

			fmt.Printf("  ✅ 저장 완료 (벡터 차원: %d)\n", len(vector))
		}

		// 최종 개수 확인
		finalCount, _ := store.Count(ctx)
		fmt.Printf("✅ DB 저장 완료! (총 %d개 문서)\n\n", finalCount)
	} else {
		finalCount, _ := store.Count(ctx)
		fmt.Printf("⚡ 기존 로컬 DB를 로드했습니다. (총 %d개 문서)\n\n", finalCount)
	}

	// RAG 검색기 초기화
	searcher, err := rag.NewSearcher(ctx, config.GeminiAPIKey, store)
	if err != nil {
		log.Fatalf("RAG 검색기 초기화 실패: %v", err)
	}
	defer searcher.Close()

	// TUI 실행
	fmt.Println("검색 모드로 진입합니다...")
	if err := ui.Run(searcher); err != nil {
		log.Fatalf("TUI 실행 실패: %v", err)
	}
}
