package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

type GeminiSession struct {
	mu             sync.RWMutex
	ConversationID string
	ResponseID     string
	ChoiceID       string
	TokenIndex     int
}

type GeminiSessionSnapshot struct {
	ConversationID string
	ResponseID     string
	ChoiceID       string
	TokenIndex     int
}

func (s *GeminiSession) Snapshot() GeminiSessionSnapshot {
	if s == nil {
		return GeminiSessionSnapshot{}
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	return GeminiSessionSnapshot{
		ConversationID: s.ConversationID,
		ResponseID:     s.ResponseID,
		ChoiceID:       s.ChoiceID,
		TokenIndex:     s.TokenIndex,
	}
}

func (s *GeminiSession) SetConversationID(conversationID string) {
	if s == nil {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.ConversationID = conversationID
}

type ChatCompletionRequest struct {
	Model          string    `json:"model"`
	Messages       []Message `json:"messages"`
	Stream         bool      `json:"stream"`
	Tools          []Tool    `json:"tools,omitempty"`
	ToolChoice     any       `json:"tool_choice,omitempty"`
	Temperature    float64   `json:"temperature,omitempty"`
	MaxTokens      int       `json:"max_tokens,omitempty"`
	ConversationID string    `json:"conversation_id,omitempty"`
}

type Tool struct {
	Type     string   `json:"type"`
	Function Function `json:"function"`
}

type Function struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description,omitempty"`
	Parameters  map[string]interface{} `json:"parameters,omitempty"`
}

type Message struct {
	Role       string      `json:"role"`
	Content    interface{} `json:"content"`
	ToolCalls  []ToolCall  `json:"tool_calls,omitempty"`
	ToolCallID string      `json:"tool_call_id,omitempty"`
}

type ContentPart struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

type ToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function FunctionCall `json:"function"`
}

type FunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type ChatCompletionResponse struct {
	ID             string   `json:"id"`
	Object         string   `json:"object"`
	Created        int64    `json:"created"`
	Model          string   `json:"model"`
	Choices        []Choice `json:"choices"`
	Usage          Usage    `json:"usage,omitempty"`
	ConversationID string   `json:"conversation_id,omitempty"`
}

type Choice struct {
	Index        int      `json:"index"`
	Message      *Message `json:"message,omitempty"`
	Delta        *Delta   `json:"delta,omitempty"`
	FinishReason *string  `json:"finish_reason,omitempty"`
}

type Delta struct {
	Role      string     `json:"role,omitempty"`
	Content   string     `json:"content,omitempty"`
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
}

type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type ModelsResponse struct {
	Object string  `json:"object"`
	Data   []Model `json:"data"`
}

type Model struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	OwnedBy string `json:"owned_by"`
}

type ErrorResponse struct {
	Error struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error"`
}

var errorCodeMap = map[int]string{
	0: "success",
	1: "invalid_request",
	2: "rate_limit_exceeded",
	3: "content_filtered",
	4: "authentication_error",
	5: "server_error",
	6: "timeout",
	7: "model_overloaded",
	8: "context_length_exceeded",
}

var modelIDMap = map[string]string{
	"gemini-3-flash":           "1640bdc9f7ef4826",
	"gemini-3":                 "1640bdc9f7ef4826",
	"gemini-2.5-flash":         "e6fa609c3fa255c0",
	"gemini-2.5-pro":           "9d8ca3786ebdfbea",
	"gemini-3.1-pro":           "9d8ca3786ebdfbea",
	"gemini-3.1":               "9d8ca3786ebdfbea",
	"gemini-2-flash":           "203e6bb81620bcfe",
	"gemini-2.0-flash":         "203e6bb81620bcfe",
	"gemini-flash":             "1640bdc9f7ef4826",
	"gemini-pro":               "9d8ca3786ebdfbea",
	"gemini-3-pro":             "9d8ca3786ebdfbea",
	"gemini-2.5-flash-preview": "e6fa609c3fa255c0",
}

func extractMessageContent(msg Message) string {
	switch v := msg.Content.(type) {
	case string:
		return v
	case []interface{}:
		var parts []string
		for _, part := range v {
			if p, ok := part.(map[string]interface{}); ok {
				if text, ok := p["text"].(string); ok {
					parts = append(parts, text)
				}
			}
		}
		return strings.Join(parts, "\n")
	default:
		if v != nil {
			return fmt.Sprintf("%v", v)
		}
		return ""
	}
}

func buildToolsPrompt(tools []Tool) string {
	if len(tools) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("\n[TOOLS]\nYou have access to the following tools. To use a tool, respond with ONLY a JSON object in this exact format (no markdown, no code blocks):\n")
	sb.WriteString("{\"name\": \"tool_name\", \"arguments\": {\"param\": \"value\"}}\n\n")
	sb.WriteString("Available tools:\n")

	for _, tool := range tools {
		if tool.Type != "function" {
			continue
		}
		sb.WriteString(fmt.Sprintf("- %s", tool.Function.Name))
		if tool.Function.Description != "" {
			sb.WriteString(fmt.Sprintf(": %s", tool.Function.Description))
		}
		if tool.Function.Parameters != nil {
			if props, ok := tool.Function.Parameters["properties"].(map[string]interface{}); ok {
				var params []string
				for k := range props {
					params = append(params, k)
				}
				sb.WriteString(fmt.Sprintf(" (params: %s)", strings.Join(params, ", ")))
			}
		}
		sb.WriteString("\n")
	}
	sb.WriteString("[/TOOLS]\n")
	return sb.String()
}

func buildPrompt(req ChatCompletionRequest) string {
	var prompt strings.Builder
	toolsPrompt := buildToolsPrompt(req.Tools)
	if toolsPrompt != "" {
		prompt.WriteString(toolsPrompt)
		prompt.WriteString("\n---\n\n")
	}
	for _, msg := range req.Messages {
		content := extractMessageContent(msg)
		switch msg.Role {
		case "system":
			prompt.WriteString(fmt.Sprintf("[System Instruction]\n%s\n[/System Instruction]\n\n", content))
		case "user":
			prompt.WriteString(fmt.Sprintf("User: %s\n\n", content))
		case "assistant":
			if len(msg.ToolCalls) > 0 {
				for _, tc := range msg.ToolCalls {
					prompt.WriteString(fmt.Sprintf("Assistant (tool_call): %s(%s)\n\n", tc.Function.Name, tc.Function.Arguments))
				}
			} else {
				prompt.WriteString(fmt.Sprintf("Assistant: %s\n\n", content))
			}
		case "tool":
			prompt.WriteString(fmt.Sprintf("Tool Result [%s]: %s\n\n", msg.ToolCallID, content))
		}
	}
	return prompt.String()
}

func parseToolCalls(content string, tools []Tool) (string, []ToolCall) {
	if len(tools) == 0 {
		return content, nil
	}

	var toolCalls []ToolCall
	cleanContent := content
	re1 := regexp.MustCompile(`(?s)\{\s*"name"\s*:\s*"([^"]+)"\s*,\s*"arguments"\s*:\s*(\{[^}]*\})\s*\}`)
	matches1 := re1.FindAllStringSubmatch(content, -1)
	for i, match := range matches1 {
		name := match[1]
		args := match[2]
		for _, t := range tools {
			if t.Function.Name == name {
				toolCalls = append(toolCalls, ToolCall{
					ID:   fmt.Sprintf("call_%s_%d", generateRandomHex(8), i),
					Type: "function",
					Function: FunctionCall{
						Name:      name,
						Arguments: args,
					},
				})
				cleanContent = strings.Replace(cleanContent, match[0], "", 1)
				break
			}
		}
	}

	if len(toolCalls) > 0 {
		return strings.TrimSpace(cleanContent), toolCalls
	}

	re2 := regexp.MustCompile("(?s)```tool_call\\s*\\n?(\\{.*?\\})\\s*```")
	matches2 := re2.FindAllStringSubmatch(content, -1)
	for i, match := range matches2 {
		var tc struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}

		jsonStr := match[1]
		if err := json.Unmarshal([]byte(jsonStr), &tc); err != nil {
			logger.Debug("解析工具调用失败: %v", err)
			continue
		}
		toolExists := false
		for _, t := range tools {
			if t.Function.Name == tc.Name {
				toolExists = true
				break
			}
		}
		if !toolExists {
			continue
		}

		toolCall := ToolCall{
			ID:   fmt.Sprintf("call_%s_%d", generateRandomHex(8), i),
			Type: "function",
			Function: FunctionCall{
				Name:      tc.Name,
				Arguments: string(tc.Arguments),
			},
		}
		toolCalls = append(toolCalls, toolCall)
		cleanContent = strings.Replace(cleanContent, match[0], "", 1)
	}

	return strings.TrimSpace(cleanContent), toolCalls
}

func buildGeminiRequest(prompt string, session *GeminiSession, modelName string, snlm0eToken string) (*http.Request, error) {
	refreshTokenIfNeeded()

	uuid := generateUUIDv4()
	modelID := modelIDMap["gemini-3-flash"]
	if id, ok := modelIDMap[modelName]; ok {
		modelID = id
		logger.Debug("正在使用模型: %s -> %s", modelName, modelID)
	}

	var contextArray []interface{}
	sessionSnapshot := session.Snapshot()
	if sessionSnapshot.ConversationID != "" {
		contextArray = []interface{}{sessionSnapshot.ConversationID, sessionSnapshot.ResponseID, sessionSnapshot.ChoiceID, nil, nil, nil, nil, nil, nil, ""}
		logger.Debug("正在使用现有会话: c=%s, r=%s, rc=%s", sessionSnapshot.ConversationID, sessionSnapshot.ResponseID, sessionSnapshot.ChoiceID)
	} else {
		contextArray = []interface{}{nil, nil, nil, nil, nil, nil, nil, nil, nil, ""}
		logger.Debug("正在开始新对话")
	}

	currentToken := snlm0eToken
	if currentToken == "" {
		currentToken = getToken()
	}

	innerArray := []interface{}{
		[]interface{}{prompt, 0, nil, nil, nil, nil, 0},
		[]interface{}{"zh-CN"},
		contextArray,
		currentToken,
		modelID,
		nil,
		[]interface{}{0},
		1, nil, nil, 9, 0, nil, nil, nil, nil, nil,
		[]interface{}{[]interface{}{1}},
		0, nil, nil, nil, nil, nil, nil, nil, nil, 1, nil, nil,
		[]interface{}{4},
		nil, nil, nil, nil, nil, nil, nil, nil, nil, nil,
		[]interface{}{2},
		nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, 0, nil, nil, nil, nil, nil,
		uuid,
		nil,
		[]interface{}{},
	}

	innerJSON, _ := json.Marshal(innerArray)
	freqData := fmt.Sprintf(`[null,%q]`, string(innerJSON))
	data := url.Values{}
	data.Set("f.req", freqData)
	endpoints := currentGeminiEndpoints()
	requestURL, err := buildGeminiRequestURL(endpoints.url)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest("POST", requestURL, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Accept", "*/*")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/146.0.0.0 Safari/537.36")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded;charset=UTF-8")
	req.Header.Set("accept-language", "zh-CN")
	if cfg := getConfigSnapshot(); cfg.Cookies != "" {
		req.Header.Set("Cookie", cfg.Cookies)
	}
	req.Header.Set("cache-control", "no-cache")
	req.Header.Set("origin", endpoints.origin)
	req.Header.Set("pragma", "no-cache")
	req.Header.Set("priority", "u=1, i")
	req.Header.Set("referer", endpoints.referer)
	req.Header.Set("sec-ch-ua", `"Chromium";v="146", "Not-A.Brand";v="24", "Google Chrome";v="146"`)
	req.Header.Set("sec-ch-ua-arch", `"x86"`)
	req.Header.Set("sec-ch-ua-bitness", `"64"`)
	req.Header.Set("sec-ch-ua-form-factors", `"Desktop"`)
	req.Header.Set("sec-ch-ua-full-version", `"146.0.7680.179"`)
	req.Header.Set("sec-ch-ua-full-version-list", `"Chromium";v="146.0.7680.179", "Not-A.Brand";v="24.0.0.0", "Google Chrome";v="146.0.7680.179"`)
	req.Header.Set("sec-ch-ua-mobile", "?0")
	req.Header.Set("sec-ch-ua-model", `""`)
	req.Header.Set("sec-ch-ua-platform", `"Windows"`)
	req.Header.Set("sec-ch-ua-platform-version", `"19.0.0"`)
	req.Header.Set("sec-ch-ua-wow64", "?0")
	req.Header.Set("sec-fetch-dest", "empty")
	req.Header.Set("sec-fetch-mode", "cors")
	req.Header.Set("sec-fetch-site", "same-origin")
	req.Header.Set("x-goog-ext-525005358-jspb", fmt.Sprintf(`["%s",1]`, uuid))
	req.Header.Set("x-goog-ext-73010989-jspb", "[0]")
	req.Header.Set("x-goog-ext-73010990-jspb", "[0]")
	req.Header.Set("x-same-domain", "1")
	randomIP := generateRandomIP()
	req.Header.Set("X-Forwarded-For", randomIP)
	req.Header.Set("X-Real-IP", randomIP)
	logger.Debug("正在使用随机 XFF IP: %s", randomIP)
	return req, nil
}

func buildGeminiRequestURL(rawURL string) (string, error) {
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}

	query := parsedURL.Query()
	if query.Get("hl") == "" {
		query.Set("hl", "zh-CN")
	}
	if query.Get("rt") == "" {
		query.Set("rt", "c")
	}
	if query.Get("bl") == "" {
		if blToken := getBLToken(); blToken != "" {
			query.Set("bl", blToken)
		}
	}
	if query.Get("f.sid") == "" {
		if fsid := getFSID(); fsid != "" {
			query.Set("f.sid", fsid)
		}
	}
	query.Set("_reqid", nextReqID())
	parsedURL.RawQuery = query.Encode()
	return parsedURL.String(), nil
}

func handleStreamResponse(w http.ResponseWriter, prompt string, model string, session *GeminiSession, tools []Tool, sessionKey string, snlm0eToken string) {
	start := time.Now()
	const maxRetries = 3
	var bodyStr, content, lastErr string

	for attempt := 1; attempt <= maxRetries; attempt++ {
		if attempt > 1 {
			logger.Info("流式请求正在进行第 %d/%d 次重试", attempt, maxRetries)
			snlm0eToken, _ = tokenManager.GetTokenForSession(sessionKey, true)
			time.Sleep(time.Duration(attempt*500) * time.Millisecond)
		}

		req, err := buildGeminiRequest(prompt, session, model, snlm0eToken)
		if err != nil {
			logger.Error("构建 Gemini 请求失败: %v", err)
			lastErr = err.Error()
			continue
		}

		logger.Debug("正在发送请求到 Gemini API...")
		resp, err := httpClient.Do(req)
		if err != nil {
			if isConnectionError(err) {
				logger.Warn("通过代理连接出错 (尝试 %d/%d): %v", attempt, maxRetries, err)
			} else {
				logger.Error("Gemini API 请求失败: %v", err)
			}
			lastErr = err.Error()
			continue
		}

		logger.Debug("Gemini API 响应状态码: %d", resp.StatusCode)
		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			bodyStr = string(body)
			logger.Error("Gemini API 返回错误状态码 %d: %s", resp.StatusCode, bodyStr)
			if isHTMLErrorResponse(bodyStr) {
				logger.Warn("检测到 HTML 错误响应，已标记会话令牌失效")
				tokenManager.MarkSessionTokenBad(sessionKey)
			}
			lastErr = fmt.Sprintf("Gemini API error: %d", resp.StatusCode)
			continue
		}

		body, err := readResponseBody(resp, "流式")
		if err != nil {
			lastErr = err.Error()
			continue
		}

		logger.Debug("流式响应体大小: %d 字节", len(body))
		bodyStr = string(body)

		if isHTMLErrorResponse(bodyStr) {
			logger.Warn("响应体中检测到 HTML 错误，已标记会话令牌失效")
			tokenManager.MarkSessionTokenBad(sessionKey)
			lastErr = "Request failed due to token issue"
			continue
		}

		content = extractFinalContent(bodyStr)
		content = filterContent(content)

		if content == "" && isEmptyAcknowledgmentResponse(bodyStr) {
			logger.Error("流式响应收到空的确认包 - 令牌可能已失效或过期")
			tokenManager.MarkSessionTokenBad(sessionKey)
			lastErr = "Gemini returned empty response - token issue"
			continue
		}

		lastErr = ""
		break
	}

	if lastErr != "" {
		logger.Error("所有 %d 次重试均失败，最后一次错误: %s", maxRetries, lastErr)
		metrics.AddRequest(false, len(prompt)/4, 0)
		writeError(w, http.StatusBadGateway, lastErr)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming not supported")
		return
	}

	updateSessionFromResponse(session, bodyStr)
	sessionSnapshot := session.Snapshot()
	sendStreamChunkWithConversation(w, flusher, model, "", "assistant", false, sessionSnapshot.ConversationID)

	if content != "" {
		logger.Debug("已提取流式内容 (长度=%d): %.100s", len(content), content)
		cleanContent, toolCalls := parseToolCalls(content, tools)
		cleanContent = filterContent(cleanContent)
		if len(toolCalls) > 0 {
			sendStreamChunkWithTools(w, flusher, model, cleanContent, toolCalls)
		} else {
			sendStreamChunk(w, flusher, model, cleanContent, "", false)
		}
	}

	inputTokens := len(prompt) / 4
	outputTokens := len(content) / 4
	metrics.AddRequest(true, inputTokens, outputTokens)
	_, toolCalls := parseToolCalls(content, tools)
	if len(toolCalls) > 0 {
		sendStreamChunkFinish(w, flusher, model, "tool_calls")
	} else {
		sendStreamChunk(w, flusher, model, "", "", true)
	}
	w.Write([]byte("data: [DONE]\n\n"))
	flusher.Flush()
	logger.Info("流式响应完成，耗时 %.3fms", float64(time.Since(start).Microseconds())/1000)
}

func sendStreamChunk(w http.ResponseWriter, flusher http.Flusher, model string, content string, role string, isFinish bool) {
	chunk := ChatCompletionResponse{
		ID:      fmt.Sprintf("chatcmpl-%d", time.Now().UnixNano()),
		Object:  "chat.completion.chunk",
		Created: time.Now().Unix(),
		Model:   model,
		Choices: []Choice{{Index: 0, Delta: &Delta{}}},
	}
	if role != "" {
		chunk.Choices[0].Delta.Role = role
	}
	if content != "" {
		chunk.Choices[0].Delta.Content = content
	}
	if isFinish {
		finishReason := "stop"
		chunk.Choices[0].FinishReason = &finishReason
	}
	jsonData, _ := json.Marshal(chunk)
	w.Write([]byte(fmt.Sprintf("data: %s\n\n", jsonData)))
	flusher.Flush()
}

func sendStreamChunkWithConversation(w http.ResponseWriter, flusher http.Flusher, model string, content string, role string, isFinish bool, conversationID string) {
	chunk := ChatCompletionResponse{
		ID:             fmt.Sprintf("chatcmpl-%d", time.Now().UnixNano()),
		Object:         "chat.completion.chunk",
		Created:        time.Now().Unix(),
		Model:          model,
		ConversationID: conversationID,
		Choices:        []Choice{{Index: 0, Delta: &Delta{}}},
	}
	if role != "" {
		chunk.Choices[0].Delta.Role = role
	}
	if content != "" {
		chunk.Choices[0].Delta.Content = content
	}
	if isFinish {
		finishReason := "stop"
		chunk.Choices[0].FinishReason = &finishReason
	}
	jsonData, _ := json.Marshal(chunk)
	w.Write([]byte(fmt.Sprintf("data: %s\n\n", jsonData)))
	flusher.Flush()
}

func sendStreamChunkWithTools(w http.ResponseWriter, flusher http.Flusher, model string, content string, toolCalls []ToolCall) {
	chunk := ChatCompletionResponse{
		ID:      fmt.Sprintf("chatcmpl-%d", time.Now().UnixNano()),
		Object:  "chat.completion.chunk",
		Created: time.Now().Unix(),
		Model:   model,
		Choices: []Choice{{Index: 0, Delta: &Delta{Content: content, ToolCalls: toolCalls}}},
	}
	jsonData, _ := json.Marshal(chunk)
	w.Write([]byte(fmt.Sprintf("data: %s\n\n", jsonData)))
	flusher.Flush()
}

func sendStreamChunkFinish(w http.ResponseWriter, flusher http.Flusher, model string, finishReason string) {
	chunk := ChatCompletionResponse{
		ID:      fmt.Sprintf("chatcmpl-%d", time.Now().UnixNano()),
		Object:  "chat.completion.chunk",
		Created: time.Now().Unix(),
		Model:   model,
		Choices: []Choice{{Index: 0, Delta: &Delta{}, FinishReason: &finishReason}},
	}
	jsonData, _ := json.Marshal(chunk)
	w.Write([]byte(fmt.Sprintf("data: %s\n\n", jsonData)))
	flusher.Flush()
}

func handleNonStreamResponse(w http.ResponseWriter, prompt string, model string, session *GeminiSession, tools []Tool, sessionKey string, snlm0eToken string) {
	start := time.Now()
	const maxRetries = 3
	var bodyStr, content, lastErr string

	for attempt := 1; attempt <= maxRetries; attempt++ {
		if attempt > 1 {
			logger.Info("非流式请求正在进行第 %d/%d 次重试", attempt, maxRetries)
			snlm0eToken, _ = tokenManager.GetTokenForSession(sessionKey, true)
			time.Sleep(time.Duration(attempt*500) * time.Millisecond)
		}

		req, err := buildGeminiRequest(prompt, session, model, snlm0eToken)
		if err != nil {
			logger.Error("构建 Gemini 请求失败: %v", err)
			lastErr = err.Error()
			continue
		}

		logger.Debug("正在发送请求到 Gemini API...")
		resp, err := httpClient.Do(req)
		if err != nil {
			if isConnectionError(err) {
				logger.Warn("通过代理连接出错 (尝试 %d/%d): %v", attempt, maxRetries, err)
			} else {
				logger.Error("Gemini API 请求失败: %v", err)
			}
			lastErr = err.Error()
			continue
		}

		body, err := readResponseBody(resp, "非流式")
		if err != nil {
			lastErr = err.Error()
			continue
		}
		logger.Debug("Gemini API 响应状态码: %d", resp.StatusCode)
		logger.Debug("响应体大小: %d 字节", len(body))
		bodyStr = string(body)

		if resp.StatusCode != http.StatusOK {
			logger.Error("Gemini API 返回错误状态码 %d: %s", resp.StatusCode, bodyStr)
			if isHTMLErrorResponse(bodyStr) {
				logger.Warn("检测到 HTML 错误响应，已标记会话令牌失效")
				tokenManager.MarkSessionTokenBad(sessionKey)
			}
			lastErr = fmt.Sprintf("Gemini API error: %d", resp.StatusCode)
			continue
		}

		if isHTMLErrorResponse(bodyStr) {
			logger.Warn("响应体中检测到 HTML 错误，已标记会话令牌失效")
			tokenManager.MarkSessionTokenBad(sessionKey)
			lastErr = "Request failed due to token issue"
			continue
		}

		content = extractFinalContent(bodyStr)
		content = filterContent(content)

		if content == "" {
			logger.Warn("从响应中提取的内容为空，响应体预览: %.500s", bodyStr)
			if isEmptyAcknowledgmentResponse(bodyStr) {
				logger.Error("收到空的确认响应 - 令牌可能已失效或过期")
				tokenManager.MarkSessionTokenBad(sessionKey)
				lastErr = "Gemini returned empty response - token issue"
				continue
			}
		}

		lastErr = ""
		break
	}

	if lastErr != "" {
		logger.Error("所有 %d 次重试均失败，最后一次错误: %s", maxRetries, lastErr)
		metrics.AddRequest(false, len(prompt)/4, 0)
		writeError(w, http.StatusBadGateway, lastErr)
		return
	}

	updateSessionFromResponse(session, bodyStr)
	cleanContent, toolCalls := parseToolCalls(content, tools)
	cleanContent = filterContent(cleanContent)

	inputTokens := len(prompt) / 4
	outputTokens := len(content) / 4
	metrics.AddRequest(true, inputTokens, outputTokens)

	logger.Info("非流式响应完成，耗时 %.3fms，内容长度: %d",
		float64(time.Since(start).Microseconds())/1000, len(content))

	finishReason := "stop"
	if len(toolCalls) > 0 {
		finishReason = "tool_calls"
	}

	sessionSnapshot := session.Snapshot()

	response := ChatCompletionResponse{
		ID:             fmt.Sprintf("chatcmpl-%d", time.Now().UnixNano()),
		Object:         "chat.completion",
		Created:        time.Now().Unix(),
		Model:          model,
		ConversationID: sessionSnapshot.ConversationID,
		Choices: []Choice{{
			Index: 0,
			Message: &Message{
				Role:      "assistant",
				Content:   cleanContent,
				ToolCalls: toolCalls,
			},
			FinishReason: &finishReason,
		}},
		Usage: Usage{
			PromptTokens:     inputTokens,
			CompletionTokens: outputTokens,
			TotalTokens:      inputTokens + outputTokens,
		},
	}

	writeJSON(w, http.StatusOK, response)
}

func updateSessionFromResponse(session *GeminiSession, body string) {
	if session == nil {
		return
	}

	snapshot := session.Snapshot()
	convRe := regexp.MustCompile(`"c_([a-f0-9]+)"`)
	if matches := convRe.FindStringSubmatch(body); len(matches) > 1 {
		snapshot.ConversationID = "c_" + matches[1]
	}

	respRe := regexp.MustCompile(`"r_([a-f0-9]+)"`)
	if matches := respRe.FindStringSubmatch(body); len(matches) > 1 {
		snapshot.ResponseID = "r_" + matches[1]
	}

	choiceRe := regexp.MustCompile(`"rc_([a-f0-9]+)"`)
	if matches := choiceRe.FindStringSubmatch(body); len(matches) > 1 {
		snapshot.ChoiceID = "rc_" + matches[1]
	}

	session.mu.Lock()
	session.ConversationID = snapshot.ConversationID
	session.ResponseID = snapshot.ResponseID
	session.ChoiceID = snapshot.ChoiceID
	session.TokenIndex = snapshot.TokenIndex
	session.mu.Unlock()

	if snapshot.ConversationID != "" {
		logger.Debug("会话已更新: c=%s, r=%s, rc=%s", snapshot.ConversationID, snapshot.ResponseID, snapshot.ChoiceID)
	}
}

func extractFinalContent(body string) string {
	if content := extractContentFromWrbFrames(body); content != "" {
		return content
	}

	var contents []string
	patterns := []struct {
		startPattern string
		arrPattern   string
		escaped      bool
	}{
		{`"rc_`, `",["`, false},
		{`\"rc_`, `\",[\"`, true},
	}

	for _, p := range patterns {
		idx := 0
		for {
			start := strings.Index(body[idx:], p.startPattern)
			if start == -1 {
				break
			}
			start += idx
			arrStart := strings.Index(body[start:], p.arrPattern)
			if arrStart == -1 {
				idx = start + len(p.startPattern)
				continue
			}
			if p.escaped {
				arrStart += start + len(p.arrPattern)
				endPos := strings.Index(body[arrStart:], `"]`)
				if endPos == -1 {
					idx = arrStart
					continue
				}
				content := body[arrStart : arrStart+endPos]
				if content != "" {
					contents = append(contents, content)
				}
				idx = arrStart + endPos + 2
			} else {
				arrStart += start + len(p.arrPattern)
				content, endPos := extractQuotedString(body[arrStart:])
				if content != "" {
					contents = append(contents, content)
				}
				idx = arrStart + endPos + 1
			}
		}
	}

	jsonArrayRe := regexp.MustCompile(`\[\s*"rc_[a-f0-9]+"\s*,\s*\[\s*"([^"\\]*(?:\\.[^"\\]*)*)"\s*\]`)
	matches := jsonArrayRe.FindAllStringSubmatch(body, -1)
	for _, match := range matches {
		if len(match) > 1 && match[1] != "" {
			contents = append(contents, match[1])
		}
	}

	return assembleContentFragments(contents)
}

func extractContentFromWrbFrames(body string) string {
	lines := strings.Split(body, "\n")
	best := ""

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || !strings.HasPrefix(line, "[[") {
			continue
		}

		var frames []interface{}
		if err := json.Unmarshal([]byte(line), &frames); err != nil {
			continue
		}

		for _, frame := range frames {
			frameItems, ok := frame.([]interface{})
			if !ok || len(frameItems) < 3 {
				continue
			}

			eventName, _ := frameItems[0].(string)
			if eventName != "wrb.fr" {
				continue
			}

			payload, _ := frameItems[2].(string)
			if payload == "" {
				continue
			}

			candidate := extractContentFromPayload(payload)
			if len(candidate) > len(best) {
				best = candidate
			}
		}
	}

	return best
}

func extractContentFromPayload(payload string) string {
	var data interface{}
	if err := json.Unmarshal([]byte(payload), &data); err != nil {
		return ""
	}

	best := ""
	visitRCNodes(data, &best)
	return strings.TrimSpace(best)
}

func visitRCNodes(node interface{}, best *string) {
	switch value := node.(type) {
	case []interface{}:
		if text, ok := extractRCText(value); ok && len(text) > len(*best) {
			*best = text
		}
		for _, item := range value {
			visitRCNodes(item, best)
		}
	}
}

func extractRCText(items []interface{}) (string, bool) {
	if len(items) < 2 {
		return "", false
	}

	id, ok := items[0].(string)
	if !ok || !strings.HasPrefix(id, "rc_") {
		return "", false
	}

	textItems, ok := items[1].([]interface{})
	if !ok || len(textItems) == 0 {
		return "", false
	}

	text, ok := textItems[0].(string)
	if !ok {
		return "", false
	}

	return strings.TrimSpace(unescapeContent(text)), text != ""
}

func assembleContentFragments(contents []string) string {
	assembled := ""
	for _, raw := range contents {
		part := strings.TrimSpace(unescapeContent(raw))
		if part == "" {
			continue
		}
		if assembled == "" {
			assembled = part
			continue
		}
		if strings.Contains(assembled, part) {
			continue
		}
		if strings.Contains(part, assembled) {
			assembled = part
			continue
		}

		if overlap := suffixPrefixOverlap(assembled, part); overlap > 0 {
			assembled += part[overlap:]
			continue
		}
		if overlap := suffixPrefixOverlap(part, assembled); overlap > 0 {
			assembled = part + assembled[overlap:]
			continue
		}

		assembled += part
	}
	return assembled
}

func suffixPrefixOverlap(left string, right string) int {
	limit := len(left)
	if len(right) < limit {
		limit = len(right)
	}
	for size := limit; size >= 8; size-- {
		if left[len(left)-size:] == right[:size] {
			return size
		}
	}
	return 0
}

func extractQuotedString(s string) (string, int) {
	if len(s) == 0 {
		return "", 0
	}
	var result strings.Builder
	i := 0
	for i < len(s) {
		if s[i] == '"' {
			return result.String(), i
		} else if s[i] == '\\' && i+1 < len(s) {
			result.WriteByte(s[i])
			result.WriteByte(s[i+1])
			i += 2
		} else {
			result.WriteByte(s[i])
			i++
		}
	}
	return result.String(), i
}

func unescapeContent(s string) string {
	if s == "" {
		return ""
	}

	current := s
	for i := 0; i < 3; i++ {
		decoded, err := strconv.Unquote(`"` + strings.ReplaceAll(current, `"`, `\"`) + `"`)
		if err != nil || decoded == current {
			break
		}
		current = decoded
	}

	replacer := strings.NewReplacer(
		`\\n`, "\n",
		`\n`, "\n",
		`\\r`, "\r",
		`\r`, "\r",
		`\\t`, "\t",
		`\t`, "\t",
		`\\\"`, `"`,
		`\"`, `"`,
		`\\\\`, `\`,
	)
	return replacer.Replace(current)
}

func readResponseBody(resp *http.Response, mode string) ([]byte, error) {
	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err == nil {
		return body, nil
	}

	if isRetryableBodyReadError(err) && len(body) > 0 {
		logger.Warn("%s响应读取不完整，已使用部分响应继续处理: %v (已读 %d 字节)", mode, err, len(body))
		return body, nil
	}

	logger.Error("读取%s响应体失败: %v", mode, err)
	return nil, err
}

func isRetryableBodyReadError(err error) bool {
	return errors.Is(err, io.ErrUnexpectedEOF) || strings.Contains(strings.ToLower(err.Error()), "unexpected eof")
}

func parseGeminiErrorCode(body string) (int, string) {
	errorRe := regexp.MustCompile(`"errorCode"\s*:\s*(\d+)`)
	if matches := errorRe.FindStringSubmatch(body); len(matches) > 1 {
		code := 0
		fmt.Sscanf(matches[1], "%d", &code)
		if msg, ok := errorCodeMap[code]; ok {
			return code, msg
		}
		return code, "unknown_error"
	}
	return 0, ""
}

func filterContent(content string) string {
	patterns := []string{
		`温馨提示：如要解锁所有应用的完整功能，请开启 \[Gemini 应用活动记录\]\([^)]+\)\s*。?\s*`,
		`温馨提示：如要解锁所有应用的完整功能，请开启 Gemini 应用活动记录[^。]*。?\s*`,
		`温馨提示[：:][^\n]*Gemini[^\n]*活动记录[^\n]*\n?`,
	}
	result := content
	for _, pattern := range patterns {
		re := regexp.MustCompile(pattern)
		result = re.ReplaceAllString(result, "")
	}
	return strings.TrimSpace(result)
}

func isEmptyAcknowledgmentResponse(body string) bool {
	hasResponseID := strings.Contains(body, `"r_`) || strings.Contains(body, `\"r_`)
	hasChoiceContent := strings.Contains(body, `"rc_`) || strings.Contains(body, `\"rc_`)
	hasNullConversation := strings.Contains(body, "[null,\"r_") || strings.Contains(body, "[null,\\\"r_")
	if hasResponseID && !hasChoiceContent && hasNullConversation {
		return true
	}
	return false
}

func isHTMLErrorResponse(body string) bool {
	htmlIndicators := []string{
		"<html",
		"<div id=\"infoDiv\"",
		"background-color:#eee",
		"我们的系统检测到",
		"异常流量",
		"自动程序发出的",
		"人机识别",
		"google.com/policies/terms",
		"服务条款",
		"display:none",
		"style.display='block'",
		"<!DOCTYPE html>",
		"<head>",
		"captcha",
		"recaptcha",
		"blocked",
		"access denied",
		"rate limit",
	}
	lower := strings.ToLower(body)
	for _, indicator := range htmlIndicators {
		if strings.Contains(lower, strings.ToLower(indicator)) {
			return true
		}
	}
	return false
}

func checkGeminiError(body string) (bool, string) {
	code, msg := parseGeminiErrorCode(body)
	if code != 0 {
		return true, fmt.Sprintf("Gemini error code %d: %s", code, msg)
	}
	if strings.Contains(body, `"error"`) {
		errorMsgRe := regexp.MustCompile(`"error"\s*:\s*\{\s*"message"\s*:\s*"([^"]+)"`)
		if matches := errorMsgRe.FindStringSubmatch(body); len(matches) > 1 {
			return true, matches[1]
		}
	}
	return false, ""
}
