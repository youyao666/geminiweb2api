package main

import (
	"fmt"
	"log"
	"net/http"
	"time"
)

func main() {
	if err := loadConfig(); err != nil {
		log.Fatalf("加载配置失败: %v", err)
	}

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
	println("- API端口: " + fmt.Sprintf("%d", getConfigSnapshot().Port))
	println("- 核心接口: /v1/chat/completions")
	println("- 监控面板: / (Dashboard)")
	println("- 遥测接口: /api/telemetry (JSON)")
	println("- 帮助文档: /help (教程 + 示例)")
	println("======================================================")

	if err := initLogger(); err != nil {
		log.Fatalf("初始化日志系统失败: %v", err)
	}

	initHTTPClient()
	tokenManager.Init()
	startTokenRefresher()
	startConfigWatcher()

	mux := http.NewServeMux()
	mux.HandleFunc("/", handleWebUI)
	mux.HandleFunc("/help", handleHelpUI)
	mux.HandleFunc("/help/", handleHelpUI)
	mux.HandleFunc("/api/telemetry", handleTelemetry)
	mux.HandleFunc("/v1/models", loggingMiddleware(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			logger.Warn("接口 /v1/models 收到无效的请求方法: %s", r.Method)
			writeError(w, http.StatusMethodNotAllowed, "请求方法不允许")
			return
		}
		now := time.Now().Unix()
		models := ModelsResponse{
			Object: "list",
			Data: []Model{
				{ID: "gemini-3-flash", Object: "model", Created: now, OwnedBy: "google"},
				{ID: "gemini-3", Object: "model", Created: now, OwnedBy: "google"},
				{ID: "gemini-3-pro", Object: "model", Created: now, OwnedBy: "google"},
				{ID: "gemini-3.1-pro", Object: "model", Created: now, OwnedBy: "google"},
				{ID: "gemini-2.5-flash", Object: "model", Created: now, OwnedBy: "google"},
				{ID: "gemini-2.5-pro", Object: "model", Created: now, OwnedBy: "google"},
				{ID: "gemini-2-flash", Object: "model", Created: now, OwnedBy: "google"},
				{ID: "gemini-2.0-flash", Object: "model", Created: now, OwnedBy: "google"},
				{ID: "gemini-flash", Object: "model", Created: now, OwnedBy: "google"},
				{ID: "gemini-pro", Object: "model", Created: now, OwnedBy: "google"},
			},
		}
		writeJSON(w, http.StatusOK, models)
	}))
	mux.HandleFunc("/v1/chat/completions", loggingMiddleware(handleChatCompletions))

	cfg := getConfigSnapshot()
	addr := fmt.Sprintf(":%d", cfg.Port)
	logger.Info("服务器已启动，正在监听 %s", addr)
	log.Fatal(http.ListenAndServe(addr, mux))
}
