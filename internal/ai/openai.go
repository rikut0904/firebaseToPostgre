package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"firebaseToPostgre/internal/migration"
)

type MappingHint struct {
	Collection       string            `json:"collection"`
	TargetTable      string            `json:"targetTable"`
	ParentCollection string            `json:"parentCollection,omitempty"`
	Fields           map[string]string `json:"fields"`
}

type MappingHints struct {
	ProjectID      string        `json:"projectId"`
	Collections    []string      `json:"collections"`
	Subcollections map[string][]string `json:"subcollections"`
	Hints          []MappingHint `json:"hints"`
}

type GenerateInput struct {
	Hints MappingHints
}

func GenerateMappings(ctx context.Context, input GenerateInput) ([]migration.CollectionMapping, error) {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		return nil, errors.New("OPENAI_API_KEY is not set")
	}
	model := os.Getenv("OPENAI_MODEL")
	if model == "" {
		model = "gpt-4o-mini"
	}

	sys, err := readPrompt()
	if err != nil {
		return nil, err
	}

	userPayload, _ := json.MarshalIndent(input.Hints, "", "  ")
	user := "以下のヒント情報からマッピングを生成してください:\n" + string(userPayload)

	reqBody := map[string]interface{}{
		"model": model,
		"input": []map[string]interface{}{
			{
				"role": "system",
				"content": []map[string]interface{}{
					{"type": "input_text", "text": sys},
				},
			},
			{
				"role": "user",
				"content": []map[string]interface{}{
					{"type": "input_text", "text": user},
				},
			},
		},
		"text": map[string]interface{}{
			"format": map[string]interface{}{
				"type":   "json_schema",
				"name":   "mapping_schema",
				"strict": true,
				"schema": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"mappings": map[string]interface{}{
							"type": "array",
							"items": map[string]interface{}{
								"type": "object",
								"properties": map[string]interface{}{
									"name": map[string]interface{}{"type": "string"},
									"sourceCollection": map[string]interface{}{"type": "string"},
									"targetTable": map[string]interface{}{"type": "string"},
									"createTable": map[string]interface{}{"type": "boolean"},
									"columns": map[string]interface{}{
										"type": "array",
										"items": map[string]interface{}{
											"type": "object",
											"properties": map[string]interface{}{
												"source": map[string]interface{}{"type": "string"},
												"target": map[string]interface{}{"type": "string"},
												"type": map[string]interface{}{"type": "string"},
												"primaryKey": map[string]interface{}{"type": "boolean"},
												"nullable": map[string]interface{}{"type": "boolean"},
											},
											"required":             []string{"source", "target", "type", "primaryKey", "nullable"},
											"additionalProperties": false,
										},
									},
								},
								"required":             []string{"name", "sourceCollection", "targetTable", "createTable", "columns"},
								"additionalProperties": false,
							},
						},
					},
					"required":             []string{"mappings"},
					"additionalProperties": false,
				},
			},
		},
	}

	body, _ := json.Marshal(reqBody)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.openai.com/v1/responses", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 45 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var errBody map[string]interface{}
		_ = json.NewDecoder(resp.Body).Decode(&errBody)
		return nil, fmt.Errorf("openai error: %v", errBody)
	}

	var parsed struct {
		Output []struct {
			Type    string `json:"type"`
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"output"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, err
	}

	var text string
	for _, out := range parsed.Output {
		if out.Type != "message" {
			continue
		}
		for _, c := range out.Content {
			if c.Type == "output_text" {
				text = c.Text
				break
			}
		}
	}
	if text == "" {
		return nil, errors.New("openai returned empty output")
	}

	var result struct {
		Mappings []migration.CollectionMapping `json:"mappings"`
	}
	if err := json.Unmarshal([]byte(text), &result); err != nil {
		return nil, fmt.Errorf("parse model output: %w", err)
	}
	return result.Mappings, nil
}

func readPrompt() (string, error) {
	paths := []string{
		"prompt/ai-mapping.md",
		filepath.Join(".", "prompt", "ai-mapping.md"),
	}
	for _, p := range paths {
		if b, err := os.ReadFile(p); err == nil {
			return string(b), nil
		}
	}
	return "", errors.New("prompt/ai-mapping.md not found")
}
