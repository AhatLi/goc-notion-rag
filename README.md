# 📚 Notion RAG 검색 시스템

Notion 페이지를 자동으로 가져와서 벡터 데이터베이스에 저장하고, RAG(Retrieval-Augmented Generation)를 사용하여 자연어 질문에 답변하는 CLI 도구입니다.

## ✨ 주요 기능

- 🔄 **자동 Notion 동기화**: Notion API를 통해 모든 페이지를 자동으로 가져와서 벡터화
- 🧠 **Gemini 임베딩**: Google Gemini Embedding API를 사용한 고품질 텍스트 임베딩
- 🔍 **유사도 기반 검색**: Cosine Similarity를 사용한 정확한 문서 검색 (유사도 0.7 이상만 표시)
- 💬 **RAG 기반 답변**: Gemini 2.5 Flash를 사용한 컨텍스트 기반 답변 생성
- ⚡ **병렬 처리**: Goroutine 기반 파이프라인으로 Notion 데이터 가져오기와 임베딩 생성을 동시에 처리
- 🛡️ **Rate Limit 처리**: API Rate Limit 에러 발생 시 자동 재시도 (30초 대기, 최대 3회)
- 📊 **데이터 조회**: 저장된 문서 목록 조회, 특정 문서 보기, 텍스트 검색 기능

## 🛠️ 기술 스택

- **언어**: Go 1.24+
- **벡터 DB**: [chromem-go](https://github.com/philippgille/chromem-go) (로컬 ChromaDB 구현)
- **임베딩 API**: Google Gemini Embedding (`gemini-embedding-001`)
- **생성 API**: Google Gemini 2.5 Flash
- **외부 API**: Notion API

## 📦 설치

### 사전 요구사항

1. **Go 1.24 이상** 설치
2. **Notion API Key** 발급
   - [Notion Integrations](https://www.notion.so/my-integrations)에서 새 Integration 생성
   - Integration에 접근할 수 있는 페이지를 공유 설정
3. **Google Gemini API Key** 발급
   - [Google AI Studio](https://makersuite.google.com/app/apikey)에서 API Key 생성

### 빌드

```bash
git clone <repository-url>
cd goc-notion-reg
go mod download
go build .
```

## ⚙️ 설정

프로젝트 루트에 `config.json` 파일을 생성하고 다음 내용을 입력하세요:

```json
{
  "notion_api_key": "your_notion_api_key_here",
  "gemini_api_key": "your_gemini_api_key_here",
  "db_path": "./my-knowledge.db"
}
```

### Notion Integration 설정

1. Notion Integration을 생성한 후, 해당 Integration을 사용할 페이지에 공유 설정
2. Integration의 "Internal Integration Token"을 `notion_api_key`에 입력

## 🚀 사용법

### 1. 초기 데이터 로드 (재인덱싱)

```bash
# 기본 설정으로 실행 (워커 5개)
go run . --reload

# 워커 수 지정 (더 빠른 처리)
go run . --reload --workers 10
```

이 명령은:
- Notion에서 모든 페이지를 가져옵니다
- 각 페이지를 청크로 분할합니다 (최소 50자)
- Gemini Embedding API로 벡터화합니다
- ChromaDB에 저장합니다

### 2. 대화형 검색 모드

```bash
go run .
```

대화형 REPL 모드로 진입합니다. 질문을 입력하면 관련 문서를 검색하고 답변을 생성합니다.

```
📚 Notion RAG 검색
질문을 입력하세요 (종료: 'exit' 또는 'q', Ctrl+C)

> 스마트 리포트 프로젝트는 무엇인가요?
🔍 검색 중...

💬 답변:
스마트 리포트는 DOCX, PPTX, HWP 등의 문서 파일을 템플릿으로 활용하여...
```

### 3. 문서 목록 조회

```bash
go run . --list
```

저장된 문서의 총 개수를 확인할 수 있습니다.

### 4. 특정 문서 보기

```bash
go run . --show <문서ID>
```

문서 ID를 지정하여 해당 문서의 전체 내용을 확인할 수 있습니다.

### 5. 텍스트 검색

```bash
go run . --search "검색어"
```

임베딩 기반 유사도 검색을 수행합니다. 유사도 0.7 이상인 결과만 표시됩니다.

## 📋 CLI 옵션

| 옵션 | 설명 | 기본값 |
|------|------|--------|
| `--reload` | Notion 데이터를 새로 가져와서 재인덱싱 | `false` |
| `--workers` | Gemini 임베딩 처리 워커 수 | `5` |
| `--list` | 저장된 문서 목록 보기 | `false` |
| `--show <ID>` | 특정 문서 ID로 내용 보기 | - |
| `--search <text>` | 텍스트로 문서 검색 (임베딩 검색) | - |

## 🏗️ 아키텍처

### 파이프라인 패턴

프로그램은 **Producer-Consumer 패턴**을 사용하여 효율적으로 처리합니다:

1. **Notion Producer**: 고루틴으로 Notion API에서 페이지를 가져와서 청킹하고 채널에 전송
2. **Gemini Consumer**: 워커 풀로 채널에서 문서를 받아 임베딩 생성 후 DB에 저장

이 방식으로 Notion API와 Gemini API를 동시에 활용하여 처리 속도를 향상시킵니다.

### 데이터 흐름

```
Notion API → 청킹 → 채널 → Gemini Embedding → ChromaDB → RAG 검색
```

### 임베딩 전략

- **저장 시**: `RETRIEVAL_DOCUMENT` task type 사용
- **검색 시**: `RETRIEVAL_QUERY` task type 사용
- **제목 포함**: 제목과 본문을 함께 임베딩하여 제목 기반 검색도 가능

## 🔧 고급 설정

### 워커 수 조정

워커 수를 늘리면 처리 속도가 향상되지만, API Rate Limit에 더 빨리 도달할 수 있습니다:

```bash
# 빠른 처리 (Rate Limit 주의)
go run . --reload --workers 20

# 안정적인 처리
go run . --reload --workers 3
```

### Rate Limit 처리

프로그램은 Rate Limit 에러를 자동으로 감지하고 처리합니다:
- Rate Limit 에러 발생 시 30초 대기
- 최대 3회 재시도
- 재시도 중 진행 상황 표시

## 📁 프로젝트 구조

```
goc-notion-reg/
├── main.go              # 메인 진입점 및 CLI 처리
├── config.go            # 설정 파일 로드
├── config.json          # API 키 설정 (gitignore 권장)
├── models/
│   └── document.go      # 문서 데이터 모델
├── notion/
│   └── loader.go        # Notion API 연동 및 청킹
├── embedding/
│   └── gemini.go        # Gemini Embedding API 연동
├── db/
│   └── store.go         # ChromaDB 저장소 관리
├── rag/
│   └── search.go        # RAG 검색 및 답변 생성
└── ui/
    └── app.go           # REPL 인터페이스
```

## 🐛 문제 해결

### "DB가 없거나 비어있습니다" 오류

```bash
go run . --reload
```

### "vectors must have the same length" 오류

임베딩 모델을 변경한 경우 기존 DB를 삭제하고 재인덱싱해야 합니다:

```bash
rm my-knowledge.db
go run . --reload
```

### Rate Limit 에러

- 워커 수를 줄여보세요: `--workers 3`
- 프로그램이 자동으로 재시도하므로 잠시 기다려보세요

### 문서가 검색되지 않음

- 유사도 0.7 미만인 결과는 필터링됩니다
- `--reload`로 최신 데이터로 재인덱싱해보세요
- 검색어를 더 구체적으로 입력해보세요

## 📝 라이선스

이 프로젝트는 개인 사용 목적으로 제작되었습니다.

## 🤝 기여

버그 리포트나 기능 제안은 이슈로 등록해주세요.

---

**Made with ❤️ using Go, Notion API, and Google Gemini**

