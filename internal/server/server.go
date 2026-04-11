package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"main/internal/config"
	"main/internal/gemini"
	"main/internal/httpclient"
	"main/internal/logging"
	"main/internal/metrics"
	"main/internal/support"
	"main/internal/token"
	"main/internal/web"
)

type Server struct {
	configStore *config.Store
	metrics     *metrics.Metrics

	loggerMu sync.RWMutex
	logger   *logging.Logger

	clientMu   sync.RWMutex
	httpClient *http.Client

	tokenManager *token.Manager

	sessions   map[string]*gemini.GeminiSession
	sessionsMu sync.RWMutex
}

type loggingResponseWriter struct {
	http.ResponseWriter
	statusCode int
	size       int
}

func New(configStore *config.Store) (*Server, error) {
	s := &Server{
		configStore: configStore,
		metrics:     metrics.New(),
		sessions:    make(map[string]*gemini.GeminiSession),
	}

	if err := s.reloadRuntime(); err != nil {
		return nil, err
	}

	s.tokenManager = token.NewManager(s.ConfigSnapshot, s.HTTPClient, s.Logger)
	gemini.Initialize(s.ConfigSnapshot, s.HTTPClient, s.Logger, s.metrics, s.tokenManager)
	return s, nil
}

func (s *Server) Run() error {
	s.printBanner()
	s.tokenManager.StartRefresher()
	s.configStore.Watch(s.reloadConfig)

	mux := http.NewServeMux()
	mux.HandleFunc("/", web.HandleIndex)
	mux.HandleFunc("/help", web.HandleHelp)
	mux.HandleFunc("/help/", web.HandleHelp)
	mux.HandleFunc("/api/telemetry", s.handleTelemetry)
	mux.HandleFunc("/v1/models", s.loggingMiddleware(s.handleModels))
	mux.HandleFunc("/v1/chat/completions", s.loggingMiddleware(s.handleChatCompletions))

	cfg := s.ConfigSnapshot()
	addr := fmt.Sprintf(":%d", cfg.Port)
	s.Logger().Info("服务器已启动，正在监听 %s", addr)
	return http.ListenAndServe(addr, mux)
}

func (s *Server) ConfigSnapshot() config.Config {
	return s.configStore.Snapshot()
}

func (s *Server) Logger() *logging.Logger {
	s.loggerMu.RLock()
	defer s.loggerMu.RUnlock()
	return s.logger
}

func (s *Server) HTTPClient() *http.Client {
	s.clientMu.RLock()
	defer s.clientMu.RUnlock()
	return s.httpClient
}

func (s *Server) reloadConfig() error {
	if err := s.configStore.Reload(); err != nil {
		return err
	}
	if err := s.reloadRuntime(); err != nil {
		return err
	}
	gemini.Initialize(s.ConfigSnapshot, s.HTTPClient, s.Logger, s.metrics, s.tokenManager)
	s.Logger().Info("配置文件已成功重载")
	return nil
}

func (s *Server) reloadRuntime() error {
	cfg := s.ConfigSnapshot()

	logger, err := logging.NewFromConfig(cfg.LogLevel, cfg.LogFile)
	if err != nil {
		return err
	}
	client := httpclient.New(cfg, logger)

	s.loggerMu.Lock()
	s.logger = logger
	s.loggerMu.Unlock()

	s.clientMu.Lock()
	s.httpClient = client
	s.clientMu.Unlock()
	return nil
}

func (s *Server) printBanner() {
	cfg := s.ConfigSnapshot()
	println("======================================================")
	println("           Gemini Web 2 API 启动成功")
	println("======================================================")
	println("作者: XxxXTeam")
	println("交流群: 1081291958")
	println("------------------------------------------------------")
	println("功能特性:")
	println("1. 兼容 OpenAI Chat API 格式")
	println("2. 支持 SOCKS5/HTTP 代理配置")
	println("3. 自动管理与刷新 Google Gemini 会话 Token")
	println("4. 启动时自动检测代理连通性")
	println("5. 配置文件 (config.json) 自动生成与热加载")
	println("------------------------------------------------------")
	println("使用说明:")
	println("- API端口: " + fmt.Sprintf("%d", cfg.Port))
	println("- API地址: http://localhost:" + fmt.Sprintf("%d", cfg.Port))
	println("- 核心接口: /v1/chat/completions")
	println("- 监控面板: / (Dashboard)")
	println("- 遥测接口: /api/telemetry (JSON)")
	println("- 帮助文档: /help (教程 + 示例)")
	println("======================================================")
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

func (s *Server) loggingMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		reqID := support.NextRequestID()
		lrw := &loggingResponseWriter{ResponseWriter: w, statusCode: http.StatusOK}

		s.Logger().Info("[#%d] 收到请求 --> %s %s %s", reqID, r.Method, r.URL.Path, r.RemoteAddr)
		next(lrw, r)

		duration := time.Since(start)
		s.Logger().Info("[#%d] 请求完成 <-- %d %s %d 字节 %.3fms",
			reqID, lrw.statusCode, http.StatusText(lrw.statusCode), lrw.size, float64(duration.Microseconds())/1000)
	}
}

func (s *Server) writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func (s *Server) writeError(w http.ResponseWriter, status int, message string) {
	resp := gemini.ErrorResponse{}
	resp.Error.Message = message
	resp.Error.Type = "invalid_request_error"
	s.writeJSON(w, status, resp)
}

func (s *Server) handleTelemetry(w http.ResponseWriter, _ *http.Request) {
	note := s.ConfigSnapshot().Note
	uptime := time.Since(s.metrics.StartTime).Seconds()
	response := map[string]interface{}{
		"status":           "running",
		"uptime_seconds":   int64(uptime),
		"total_requests":   atomic.LoadUint64(&s.metrics.TotalRequests),
		"success_requests": atomic.LoadUint64(&s.metrics.SuccessRequests),
		"failed_requests":  atomic.LoadUint64(&s.metrics.FailedRequests),
		"rpm":              s.metrics.GetRPM(),
		"input_tokens":     atomic.LoadUint64(&s.metrics.InputTokens),
		"output_tokens":    atomic.LoadUint64(&s.metrics.OutputTokens),
		"note":             note,
	}
	s.writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleModels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.Logger().Warn("接口 /v1/models 收到无效的请求方法: %s", r.Method)
		s.writeError(w, http.StatusMethodNotAllowed, "请求方法不允许")
		return
	}

	now := time.Now().Unix()
	models := gemini.ModelsResponse{
		Object: "list",
		Data: []gemini.Model{
			{ID: "gemini-3-flash", Object: "model", Created: now, OwnedBy: "google"},
			{ID: "gemini-3", Object: "model", Created: now, OwnedBy: "google"},
			{ID: "gemini-3-flash-thinking", Object: "model", Created: now, OwnedBy: "google"},
			{ID: "gemini-3-flash-plus", Object: "model", Created: now, OwnedBy: "google"},
			{ID: "gemini-3-flash-thinking-plus", Object: "model", Created: now, OwnedBy: "google"},
			{ID: "gemini-3-flash-advanced", Object: "model", Created: now, OwnedBy: "google"},
			{ID: "gemini-3-pro", Object: "model", Created: now, OwnedBy: "google"},
			{ID: "gemini-3-pro-advanced", Object: "model", Created: now, OwnedBy: "google"},
			{ID: "gemini-3-pro-plus", Object: "model", Created: now, OwnedBy: "google"},
			{ID: "gemini-3.1", Object: "model", Created: now, OwnedBy: "google"},
			{ID: "gemini-3.1-pro", Object: "model", Created: now, OwnedBy: "google"},
			{ID: "gemini-2.5-flash", Object: "model", Created: now, OwnedBy: "google"},
			{ID: "gemini-2.5-pro", Object: "model", Created: now, OwnedBy: "google"},
			{ID: "gemini-2-flash", Object: "model", Created: now, OwnedBy: "google"},
			{ID: "gemini-2.0-flash", Object: "model", Created: now, OwnedBy: "google"},
			{ID: "gemini-flash", Object: "model", Created: now, OwnedBy: "google"},
			{ID: "gemini-pro", Object: "model", Created: now, OwnedBy: "google"},
		},
	}
	s.writeJSON(w, http.StatusOK, models)
}

func (s *Server) getOrCreateSession(sessionKey, conversationID string) (*gemini.GeminiSession, bool) {
	s.sessionsMu.RLock()
	session, exists := s.sessions[sessionKey]
	s.sessionsMu.RUnlock()
	if exists {
		return session, false
	}

	s.sessionsMu.Lock()
	defer s.sessionsMu.Unlock()

	if session, exists = s.sessions[sessionKey]; exists {
		return session, false
	}

	session = &gemini.GeminiSession{}
	if conversationID != "" {
		session.SetConversationID(conversationID)
	}
	s.sessions[sessionKey] = session
	return session, true
}

func (s *Server) handleChatCompletions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.Logger().Warn("接口 /v1/chat/completions 收到无效的请求方法: %s", r.Method)
		s.writeError(w, http.StatusMethodNotAllowed, "请求方法不允许")
		return
	}

	auth := r.Header.Get("Authorization")
	if auth == "" {
		s.Logger().Warn("来自 %s 的请求缺失 Authorization 请求头", r.RemoteAddr)
		s.writeError(w, http.StatusUnauthorized, "缺失 authorization 请求头")
		return
	}

	cfg := s.ConfigSnapshot()
	auth = strings.TrimPrefix(auth, "Bearer ")
	if auth != cfg.APIKey {
		s.Logger().Warn("来自 %s 的请求使用了无效的 API Key", r.RemoteAddr)
		s.writeError(w, http.StatusUnauthorized, "无效的 api key")
		return
	}

	var req gemini.ChatCompletionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.Logger().Error("解析请求体失败: %v", err)
		s.writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	s.Logger().Info("对话请求: 模型=%s, 消息数=%d, 流式=%v", req.Model, len(req.Messages), req.Stream)
	s.Logger().Debug("请求消息内容: %+v", req.Messages)

	sessionKey := r.Header.Get("X-Session-ID")
	if sessionKey == "" {
		sessionKey = fmt.Sprintf("default-%s", r.RemoteAddr)
	}
	if req.ConversationID != "" {
		sessionKey = req.ConversationID
	}

	session, isNewSession := s.getOrCreateSession(sessionKey, req.ConversationID)
	sessionSnapshot := session.Snapshot()

	if req.ConversationID != "" {
		if sessionSnapshot.ConversationID != req.ConversationID {
			session.SetConversationID(req.ConversationID)
		}
		if isNewSession {
			s.Logger().Debug("根据 conversation_id 创建了新会话: %s", req.ConversationID)
			isNewSession = false
		} else {
			s.Logger().Debug("正在恢复对话: %s", req.ConversationID)
		}
	} else if isNewSession {
		s.Logger().Debug("创建了新会话: %s", sessionKey)
	} else {
		s.Logger().Debug("正在使用现有会话: %s (c=%s)", sessionKey, sessionSnapshot.ConversationID)
	}

	snlm0eToken, _ := s.tokenManager.GetTokenForSession(sessionKey, isNewSession)
	prompt := gemini.BuildPrompt(req)

	if req.Stream {
		s.Logger().Debug("开始流式响应")
		gemini.HandleStreamResponse(w, prompt, req.Model, session, req.Tools, sessionKey, snlm0eToken, s.writeError)
		return
	}

	s.Logger().Debug("开始非流式响应")
	gemini.HandleNonStreamResponse(w, prompt, req.Model, session, req.Tools, sessionKey, snlm0eToken, s.writeError, s.writeJSON)
}
