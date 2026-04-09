package main

import (
	"log"

	"main/internal/config"
	"main/internal/server"
)

func main() {
	cfgStore := config.NewStore("config.json")
	if err := cfgStore.Load(); err != nil {
		log.Fatalf("加载配置失败: %v", err)
	}

	srv, err := server.New(cfgStore)
	if err != nil {
		log.Fatalf("初始化服务失败: %v", err)
	}

	if err := srv.Run(); err != nil {
		log.Fatal(err)
	}
}
