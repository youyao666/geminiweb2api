package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"time"
)

type loggingResponseWriter struct {
	http.ResponseWriter
	statusCode int
	size       int
}

func (lrw *loggingResponseWriter) WriteHeader(code int) {
	lrw.statusCode = code
	lrw.ResponseWriter.WriteHeader(code)
}

func (lrw *loggingResponseWriter) Write(b []byte) (int, error) {
	size, err := lrw.ResponseWriter.Write(b)
	lrw.size += size
	return size, err
}

func (lrw *loggingResponseWriter) Flush() {
	if flusher, ok := lrw.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func loggingMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		reqID := getRequestID()

		lrw := &loggingResponseWriter{ResponseWriter: w, statusCode: http.StatusOK}

		logger.Info("[#%d] 收到请求 --> %s %s %s", reqID, r.Method, r.URL.Path, r.RemoteAddr)

		next(lrw, r)

		duration := time.Since(start)
		logger.Info("[#%d] 请求完成 <-- %d %s %d 字节 %.3fms",
			reqID, lrw.statusCode, http.StatusText(lrw.statusCode), lrw.size, float64(duration.Microseconds())/1000)
	}
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, message string) {
	resp := ErrorResponse{}
	resp.Error.Message = message
	resp.Error.Type = "invalid_request_error"
	writeJSON(w, status, resp)
}

func handleTelemetry(w http.ResponseWriter, r *http.Request) {
	note := getConfigSnapshot().Note

	uptime := time.Since(metrics.StartTime).Seconds()
	response := map[string]interface{}{
		"status":           "running",
		"uptime_seconds":   int64(uptime),
		"total_requests":   atomic.LoadUint64(&metrics.TotalRequests),
		"success_requests": atomic.LoadUint64(&metrics.SuccessRequests),
		"failed_requests":  atomic.LoadUint64(&metrics.FailedRequests),
		"rpm":              metrics.GetRPM(),
		"input_tokens":     atomic.LoadUint64(&metrics.InputTokens),
		"output_tokens":    atomic.LoadUint64(&metrics.OutputTokens),
		"note":             note,
	}
	writeJSON(w, http.StatusOK, response)
}

func handleWebUI(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(indexHTML)
}

func handleHelpUI(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/help" && r.URL.Path != "/help/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(helpHTML)
}

var sessions = make(map[string]*GeminiSession)

func handleChatCompletions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		logger.Warn("接口 /v1/chat/completions 收到无效的请求方法: %s", r.Method)
		writeError(w, http.StatusMethodNotAllowed, "请求方法不允许")
		return
	}

	auth := r.Header.Get("Authorization")
	if auth == "" {
		logger.Warn("来自 %s 的请求缺失 Authorization 请求头", r.RemoteAddr)
		writeError(w, http.StatusUnauthorized, "缺失 authorization 请求头")
		return
	}
	cfg := getConfigSnapshot()
	auth = strings.TrimPrefix(auth, "Bearer ")
	if auth != cfg.APIKey {
		logger.Warn("来自 %s 的请求使用了无效的 API Key", r.RemoteAddr)
		writeError(w, http.StatusUnauthorized, "无效的 api key")
		return
	}

	var req ChatCompletionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		logger.Error("解析请求体失败: %v", err)
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	logger.Info("对话请求: 模型=%s, 消息数=%d, 流式=%v", req.Model, len(req.Messages), req.Stream)
	logger.Debug("请求消息内容: %+v", req.Messages)

	sessionKey := r.Header.Get("X-Session-ID")
	if sessionKey == "" {
		sessionKey = fmt.Sprintf("default-%s", r.RemoteAddr)
	}

	if req.ConversationID != "" {
		sessionKey = req.ConversationID
	}

	session, exists := sessions[sessionKey]
	isNewSession := !exists

	if req.ConversationID != "" && exists {
		logger.Debug("正在恢复对话: %s", req.ConversationID)
	} else if req.ConversationID != "" && !exists {
		session = &GeminiSession{ConversationID: req.ConversationID}
		sessions[sessionKey] = session
		logger.Debug("根据 conversation_id 创建了新会话: %s", req.ConversationID)
		isNewSession = false
	} else if !exists {
		session = &GeminiSession{}
		sessions[sessionKey] = session
		logger.Debug("创建了新会话: %s", sessionKey)
	} else {
		logger.Debug("正在使用现有会话: %s (c=%s)", sessionKey, session.ConversationID)
	}

	snlm0eToken, _ := tokenManager.GetTokenForSession(sessionKey, isNewSession)
	prompt := buildPrompt(req)

	if req.Stream {
		logger.Debug("开始流式响应")
		handleStreamResponse(w, prompt, req.Model, session, req.Tools, sessionKey, snlm0eToken)
	} else {
		logger.Debug("开始非流式响应")
		handleNonStreamResponse(w, prompt, req.Model, session, req.Tools, sessionKey, snlm0eToken)
	}
}
