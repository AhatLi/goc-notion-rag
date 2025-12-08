package ui

import (
	"fmt"
	"strings"

	"goc-notion-reg/rag"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var (
	titleStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FAFAFA")).
			Background(lipgloss.Color("#7D56F4")).
			Padding(0, 1).
			MarginBottom(1)

	questionStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FF6B6B")).
			MarginBottom(1).
			PaddingLeft(2)

	answerStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#4ECDC4")).
			MarginTop(1).
			PaddingLeft(2).
			Width(80)

	errorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FF0000")).
			MarginTop(1).
			PaddingLeft(2)

	loadingStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FFD93D")).
			MarginTop(1).
			PaddingLeft(2)
)

// Model TUI 애플리케이션 모델
type Model struct {
	searcher *rag.Searcher
	question string
	answer   string
	err      error
	loading  bool
	quitting bool
	width    int
	height   int
}

// NewModel 새로운 TUI 모델을 생성합니다
func NewModel(searcher *rag.Searcher) *Model {
	return &Model{
		searcher: searcher,
		question: "",
		answer:   "",
		loading:  false,
		quitting: false,
	}
}

// Init bubbletea 초기화 함수
func (m *Model) Init() tea.Cmd {
	return nil
}

// Update bubbletea 업데이트 함수
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyMsg:
		if m.loading {
			return m, nil
		}

		switch msg.String() {
		case "ctrl+c", "q":
			m.quitting = true
			return m, tea.Quit

		case "enter":
			if strings.TrimSpace(m.question) == "" {
				return m, nil
			}
			if strings.TrimSpace(m.question) == "exit" {
				m.quitting = true
				return m, tea.Quit
			}
			// 검색 시작
			m.loading = true
			m.answer = ""
			m.err = nil
			return m, m.search(m.question)

		case "backspace":
			if len(m.question) > 0 {
				m.question = m.question[:len(m.question)-1]
			}
			return m, nil

		default:
			// 일반 텍스트 입력
			if len(msg.Runes) > 0 {
				m.question += string(msg.Runes)
			}
			return m, nil
		}

	case searchResultMsg:
		m.loading = false
		if msg.err != nil {
			m.err = msg.err
		} else {
			m.answer = msg.answer
		}
		m.question = "" // 질문 초기화
		return m, nil
	}

	return m, nil
}

// View bubbletea 뷰 함수
func (m *Model) View() string {
	if m.quitting {
		return "\n👋 안녕히 가세요!\n\n"
	}

	var b strings.Builder

	// 제목
	b.WriteString(titleStyle.Render("📚 Notion RAG 검색"))
	b.WriteString("\n\n")

	// 입력 필드
	b.WriteString("질문 입력 (Enter: 검색, q: 종료):\n")
	b.WriteString("> " + m.question)
	if !m.loading {
		b.WriteString("_") // 커서 표시
	}
	b.WriteString("\n\n")

	// 로딩 상태
	if m.loading {
		b.WriteString(loadingStyle.Render("🔍 검색 중..."))
		b.WriteString("\n")
		return b.String()
	}

	// 에러 표시
	if m.err != nil {
		b.WriteString(errorStyle.Render(fmt.Sprintf("❌ 오류: %v", m.err)))
		b.WriteString("\n")
		return b.String()
	}

	// 답변 표시
	if m.answer != "" {
		b.WriteString(questionStyle.Render("💬 답변:"))
		b.WriteString("\n")
		// 답변을 여러 줄로 나누어 표시
		lines := strings.Split(m.answer, "\n")
		for _, line := range lines {
			b.WriteString(answerStyle.Render(line))
			b.WriteString("\n")
		}
	}

	return b.String()
}

// searchResultMsg 검색 결과 메시지
type searchResultMsg struct {
	answer string
	err    error
}

// search 검색을 수행하는 커맨드
func (m *Model) search(question string) tea.Cmd {
	return func() tea.Msg {
		answer, err := m.searcher.Search(question)
		return searchResultMsg{
			answer: answer,
			err:    err,
		}
	}
}

// Run TUI 애플리케이션을 실행합니다
func Run(searcher *rag.Searcher) error {
	model := NewModel(searcher)
	p := tea.NewProgram(model, tea.WithAltScreen())
	_, err := p.Run()
	return err
}
