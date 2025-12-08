package main

import (
	"encoding/json"
	"fmt"
	"os"
)

// Config 애플리케이션 설정 구조체
type Config struct {
	NotionAPIKey string `json:"notion_api_key"`
	GeminiAPIKey string `json:"gemini_api_key"`
	DBPath       string `json:"db_path"`
}

// LoadConfig config.json 파일에서 설정을 로드합니다
func LoadConfig() (*Config, error) {
	configPath := "config.json"

	// 파일이 존재하지 않으면 기본 설정으로 생성
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		defaultConfig := &Config{
			DBPath: "./my-knowledge.db",
		}

		// 기본 설정 파일 생성
		data, err := json.MarshalIndent(defaultConfig, "", "  ")
		if err != nil {
			return nil, fmt.Errorf("설정 파일 생성 실패: %w", err)
		}

		if err := os.WriteFile(configPath, data, 0644); err != nil {
			return nil, fmt.Errorf("설정 파일 쓰기 실패: %w", err)
		}

		return nil, fmt.Errorf("config.json 파일이 생성되었습니다. Notion API Key와 Gemini API Key를 설정해주세요")
	}

	// 파일 읽기
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("설정 파일 읽기 실패: %w", err)
	}

	var config Config
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("설정 파일 파싱 실패: %w", err)
	}

	// 필수 값 검증
	if config.NotionAPIKey == "" {
		return nil, fmt.Errorf("config.json에 notion_api_key가 설정되지 않았습니다")
	}

	if config.GeminiAPIKey == "" {
		return nil, fmt.Errorf("config.json에 gemini_api_key가 설정되지 않았습니다")
	}

	// DB 경로 기본값 설정
	if config.DBPath == "" {
		config.DBPath = "./my-knowledge.db"
	}

	return &config, nil
}
