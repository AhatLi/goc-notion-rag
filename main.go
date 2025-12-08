package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"goc-notion-reg/db"
	"goc-notion-reg/embedding"
	"goc-notion-reg/models"
	"goc-notion-reg/notion"
	"goc-notion-reg/rag"
	"goc-notion-reg/ui"
)

func main() {
	// 플래그 파싱
	reload := flag.Bool("reload", false, "Notion 데이터를 새로 가져옵니다")
	workers := flag.Int("workers", 5, "Gemini 임베딩 처리 워커 수 (기본값: 5)")
	list := flag.Bool("list", false, "저장된 문서 목록 보기 (제목으로 검색)")
	show := flag.String("show", "", "특정 문서 ID로 내용 보기")
	searchText := flag.String("search", "", "텍스트로 문서 검색 (임베딩 검색)")
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

	// 데이터 조회 모드
	if *list {
		showDocumentList(ctx, store, count)
		return
	}

	if *show != "" {
		showDocumentByID(ctx, store, *show)
		return
	}

	if *searchText != "" {
		searchDocuments(ctx, store, config.GeminiAPIKey, *searchText)
		return
	}

	// 리로드 모드 또는 DB가 비어있는 경우
	if *reload || !dbExists || count == 0 {
		if !*reload && (!dbExists || count == 0) {
			fmt.Println("⚠️  DB가 없거나 비어있습니다. --reload 옵션으로 데이터를 생성해주세요.")
			fmt.Println("   또는 --reload 플래그를 사용하여 자동으로 데이터를 가져옵니다.")
			os.Exit(1)
		}

		fmt.Println("🔄 Notion에서 데이터를 가져오는 중...")
		fmt.Printf("⚙️  워커 수: %d\n", *workers)

		// Notion 로더 초기화
		loader := notion.NewLoader(config.NotionAPIKey)

		// 파이프라인 패턴으로 처리
		if err := processDocumentsPipeline(ctx, loader, config.GeminiAPIKey, store, *workers); err != nil {
			log.Fatalf("문서 처리 실패: %v", err)
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

	// REPL 실행
	fmt.Println("검색 모드로 진입합니다...")
	if err := ui.Run(searcher); err != nil {
		log.Fatalf("REPL 실행 실패: %v", err)
	}
}

// processDocumentsPipeline 파이프라인 패턴으로 문서를 처리합니다
// Notion Producer 고루틴과 Gemini Consumer 워커 풀을 동시에 실행합니다
func processDocumentsPipeline(
	ctx context.Context,
	loader *notion.Loader,
	geminiAPIKey string,
	store *db.Store,
	workerCount int,
) error {
	// 문서 채널 생성 (버퍼 크기는 워커 수의 2배)
	docChan := make(chan *models.Document, workerCount*2)

	// 통계 변수
	var (
		processedCount int64
		successCount   int64
		errorCount     int64
		skippedCount   int64
	)

	// 진행 상황 출력용 ticker
	progressTicker := time.NewTicker(2 * time.Second)
	defer progressTicker.Stop()

	// 진행 상황 출력 고루틴
	progressDone := make(chan bool)
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-progressDone:
				return
			case <-progressTicker.C:
				processed := atomic.LoadInt64(&processedCount)
				success := atomic.LoadInt64(&successCount)
				errors := atomic.LoadInt64(&errorCount)
				skipped := atomic.LoadInt64(&skippedCount)
				fmt.Printf("📊 진행 상황: 처리됨 %d (성공: %d, 실패: %d, 건너뜀: %d)\n",
					processed, success, errors, skipped)
			}
		}
	}()

	// 임베딩 생성기 풀 생성 (각 워커가 독립적인 임베딩 생성기 사용)
	embedders := make([]*embedding.Embedder, workerCount)
	for i := 0; i < workerCount; i++ {
		embedder, err := embedding.NewEmbedder(ctx, geminiAPIKey)
		if err != nil {
			// 이미 생성된 임베딩 생성기 정리
			for j := 0; j < i; j++ {
				embedders[j].Close()
			}
			return fmt.Errorf("임베딩 생성기 초기화 실패: %w", err)
		}
		embedders[i] = embedder
	}
	defer func() {
		for _, embedder := range embedders {
			if embedder != nil {
				embedder.Close()
			}
		}
	}()

	// Gemini Consumer 워커 풀 시작
	var wg sync.WaitGroup
	for i := 0; i < workerCount; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			embedder := embedders[workerID]

			for doc := range docChan {
				// 콘텐츠 길이 확인
				contentLen := len([]rune(doc.Content))
				if contentLen < 50 {
					atomic.AddInt64(&skippedCount, 1)
					atomic.AddInt64(&processedCount, 1)
					continue
				}

				// 임베딩 생성 (제목 + 본문을 함께 임베딩하여 제목 기반 검색도 가능하도록)
				embeddingText := doc.Content
				if doc.Title != "" {
					// 제목을 본문 앞에 추가하여 임베딩에 포함
					embeddingText = doc.Title + "\n\n" + doc.Content
				}
				vector, err := embedder.EmbedText(embeddingText, "RETRIEVAL_DOCUMENT")
				if err != nil {
					log.Printf("⚠️  [워커 %d] 문서 %s 임베딩 실패: %v", workerID, doc.ID, err)
					atomic.AddInt64(&errorCount, 1)
					atomic.AddInt64(&processedCount, 1)
					continue
				}

				doc.Vector = vector

				// DB에 저장
				if err := store.AddDocument(ctx, doc); err != nil {
					log.Printf("⚠️  [워커 %d] 문서 %s 저장 실패: %v", workerID, doc.ID, err)
					atomic.AddInt64(&errorCount, 1)
					atomic.AddInt64(&processedCount, 1)
					continue
				}

				atomic.AddInt64(&successCount, 1)
				atomic.AddInt64(&processedCount, 1)
			}
		}(i)
	}

	// Notion Producer 고루틴 시작
	var producerErr error
	var producerWg sync.WaitGroup
	producerWg.Add(1)
	go func() {
		defer producerWg.Done()
		fmt.Println("🧠 Notion Producer 시작 - Gemini Consumer와 병렬 처리 중...")
		producerErr = loader.FetchAllPagesStream(ctx, docChan)
		if producerErr != nil {
			log.Printf("⚠️  Notion Producer 오류: %v", producerErr)
		}
	}()

	// 모든 워커가 완료될 때까지 대기
	wg.Wait()

	// 진행 상황 출력 중지
	progressTicker.Stop()
	progressDone <- true

	// Producer 완료 대기
	producerWg.Wait()

	// 최종 통계 출력
	finalProcessed := atomic.LoadInt64(&processedCount)
	finalSuccess := atomic.LoadInt64(&successCount)
	finalErrors := atomic.LoadInt64(&errorCount)
	finalSkipped := atomic.LoadInt64(&skippedCount)

	fmt.Printf("\n📊 최종 결과: 처리됨 %d (성공: %d, 실패: %d, 건너뜀: %d)\n",
		finalProcessed, finalSuccess, finalErrors, finalSkipped)

	if producerErr != nil {
		return producerErr
	}

	return nil
}

// showDocumentList 저장된 문서 목록을 보여줍니다
func showDocumentList(ctx context.Context, store *db.Store, totalCount int) {
	fmt.Printf("📚 저장된 문서 총 개수: %d개\n\n", totalCount)
	fmt.Println("⚠️  참고: chromem-go의 API 제한으로 인해 모든 문서 목록을 직접 조회할 수 없습니다.")
	fmt.Println("   대신 --search 옵션을 사용하여 특정 키워드로 검색할 수 있습니다.")
	fmt.Println("\n사용 예:")
	fmt.Println("  go run . --search \"스마트 리포트\"")
	fmt.Println("  go run . --show <문서ID>")
}

// showDocumentByID 특정 문서 ID로 내용을 보여줍니다
func showDocumentByID(ctx context.Context, store *db.Store, docID string) {
	doc, err := store.GetByID(ctx, docID)
	if err != nil {
		log.Fatalf("문서 조회 실패: %v", err)
	}

	fmt.Printf("📄 문서 ID: %s\n", doc.ID)
	if doc.Title != "" {
		fmt.Printf("📌 제목: %s\n", doc.Title)
	}
	if doc.ParentPageID != "" {
		fmt.Printf("🔗 원본 페이지 ID: %s\n", doc.ParentPageID)
	}
	if doc.Meta != nil {
		if url, ok := doc.Meta["url"]; ok {
			fmt.Printf("🌐 URL: %s\n", url)
		}
		if created, ok := doc.Meta["created"]; ok {
			fmt.Printf("📅 생성일: %s\n", created)
		}
		if lastEdit, ok := doc.Meta["last_edit"]; ok {
			fmt.Printf("✏️  수정일: %s\n", lastEdit)
		}
	}
	fmt.Printf("\n📝 내용 (%d자):\n", len([]rune(doc.Content)))
	fmt.Println("---")
	fmt.Println(doc.Content)
	fmt.Println("---")
}

// searchDocuments 텍스트로 문서를 검색합니다
func searchDocuments(ctx context.Context, store *db.Store, geminiAPIKey string, query string) {
	fmt.Printf("🔍 검색어: \"%s\"\n\n", query)

	// 임베딩 생성기 초기화
	embedder, err := embedding.NewEmbedder(ctx, geminiAPIKey)
	if err != nil {
		log.Fatalf("임베딩 생성기 초기화 실패: %v", err)
	}
	defer embedder.Close()

	// 검색 쿼리를 임베딩으로 변환
	queryVector, err := embedder.EmbedText(query, "RETRIEVAL_QUERY")
	if err != nil {
		log.Fatalf("검색 쿼리 임베딩 실패: %v", err)
	}

	// 검색 실행
	documents, err := store.Search(ctx, queryVector, 10) // Top 10
	if err != nil {
		log.Fatalf("검색 실패: %v", err)
	}

	if len(documents) == 0 {
		fmt.Println("검색 결과가 없습니다.")
		return
	}

	fmt.Printf("📊 검색 결과: %d개 문서\n\n", len(documents))
	for i, doc := range documents {
		fmt.Printf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")
		fmt.Printf("결과 %d:\n", i+1)
		if doc.Title != "" {
			fmt.Printf("제목: %s\n", doc.Title)
		}
		fmt.Printf("ID: %s\n", doc.ID)
		if doc.ParentPageID != "" {
			fmt.Printf("원본 페이지: %s\n", doc.ParentPageID)
		}
		if doc.Meta != nil {
			if url, ok := doc.Meta["url"]; ok {
				fmt.Printf("URL: %s\n", url)
			}
		}
		fmt.Printf("\n내용 (%d자):\n", len([]rune(doc.Content)))
		fmt.Println("---")
		// 내용이 길면 처음 500자만 표시
		content := doc.Content
		if len([]rune(content)) > 500 {
			content = string([]rune(content)[:500]) + "..."
		}
		fmt.Println(content)
		fmt.Println("---")
		fmt.Println()
	}
}
