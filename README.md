# Gemini Web API

`geminiweb2api` 是一个用 Go 编写的轻量级代理服务：对外提供 OpenAI 兼容的 `/v1/chat/completions` 与 `/v1/models` 接口，后端转发到 Gemini 网页端并支持流式返回、会话保持与简单遥测。

## 目录

- [简介](#简介)
- [准备工作](#准备工作)
- [安装与启动](#安装与启动)
- [完整配置](#完整配置)
- [token / cookies 获取与配置](#token--cookies-获取与配置)
- [运行与调用示例](#运行与调用示例)
- [常见问题与排查](#常见问题与排查)

## 简介

本项目旨在让 Gemini 网页端模型通过本地服务以 OpenAI 兼容方式被调用。你只需要在本机运行该服务，并用自定义 `api_key` 保护本地接口，业务端即可像调用 OpenAI API 一样调用它。

## 准备工作

- Go 1.20+ 环境
- 可访问 Gemini 网页首页的网络（若遇访问问题，建议配置 `proxy`）
- 一个用来保护本服务的本地 `api_key`

## 安装与启动

1. 进入项目目录：

```bash
cd e:/Project/AI/All2API/geminiweb2api
```

2. 安装依赖并运行：

```bash
go mod tidy
go run .
```

3. 默认监听地址：

- `http://127.0.0.1:8080`

4. 验证服务是否启动：

```bash
curl -s "http://127.0.0.1:8080/api/telemetry"
curl -s "http://127.0.0.1:8080/v1/models"
```

## 完整配置

项目根目录下使用 `config.json`：

```json
{
  "api_key": "your-api-key-here",
  "token": "",
  "cookies": "",
  "tokens": null,
  "proxy": "",
  "gemini_url": "",
  "gemini_home_url": "",
  "port": 8080,
  "log_file": "",
  "log_level": "info",
  "note": [
    "Auto-generated config"
  ]
}
```

### 字段说明

- `api_key` (必填)
  - 本服务的访问密钥。调用 `/v1/chat/completions` 等接口时必须在 Header 中指定 `Authorization: Bearer <api_key>`。
- `token`
  - 手动填入 Gemini 的匿名令牌 `SNlM0e`。
  - 仅在自动获取失败时作为备选方案，正常情况下不建议手动维护。
- `cookies`
  - Gemini 网页端 Cookie 字符串，用于更稳定地抓取页面令牌。
  - 有可登录账号或网络环境受限时推荐填写。
- `tokens`
  - 预留字段，目前代码中未使用。
- `proxy`
  - 显式代理地址，例如 `http://127.0.0.1:7890`。
  - 若无法直接访问 Gemini，可配置该项；程序也会优先读取环境变量 `HTTP_PROXY` / `HTTPS_PROXY` / `ALL_PROXY`。
- `gemini_url`
  - 覆盖 Gemini 流式生成接口地址，用于反代或自建代理时配置。
- `gemini_home_url`
  - 覆盖 Gemini 首页地址，用于反代环境或特定网络条件下抓取 token。
- `port`
  - 本服务监听端口，默认 `8080`。
- `log_file`
  - 日志文件路径。为空时输出到 stdout。
- `log_level`
  - 日志级别：`debug`、`info`、`warn`、`error`。
- `note`
  - 可以放一些备注字符串，在 `/api/telemetry` 中显示。

### 配置热加载

程序会每 5 秒检测一次 `config.json` 改动并自动重载，无需重启服务。修改 `config.json` 后请保存文件，程序会打印日志提示重载结果。

## token / cookies 获取与配置

### 1. 默认自动获取

- 程序默认会从 Gemini 首页自动抓取匿名 `SNlM0e` 令牌。
- 如果不填写 `cookies`，仍然会尝试通过匿名方式获取令牌；但使用 `cookies` 能提升稳定性和刷新成功率。

### 2. 手动获取 `SNlM0e` token

当自动获取失败时，可手动从浏览器获得 `SNlM0e`：

1. 打开浏览器访问 `https://gemini.google.com/`（或你在 `gemini_home_url` 中配置的地址）。
2. 打开开发者工具。
3. 在 `Elements` / `Sources` / `Network` 中搜索 `SNlM0e`。
4. 找到类似 `"SNlM0e":"..."` 的值并复制 `...` 部分。
5. 写入 `config.json` 的 `token` 字段：

```json
{
  "token": "SNlM0e-example-value"
}
```

### 3. Cookies 获取方式

如果你的访问环境需要登录或出现防护页面，建议同时填写 `cookies`：

1. 登录 Gemini 网页或访问 Gemini 首页，使浏览器处于正常可用状态。
2. 打开开发者工具 -> `Application` / `存储` -> `Cookies`。
3. 将 `Cookie` 字符串复制完整，如：

```
SID=...; APISID=...; SAPISID=...; ...
```

4. 写入 `config.json`：

```json
{
  "cookies": "SID=...; APISID=...; SAPISID=...; ..."
}
```

> 注意：不要将真实 Cookie 上传到公共仓库，建议用私有方式管理。

### 4. `token` 与 `cookies` 的使用逻辑

- `cookies` 存在时，程序会使用它访问 Gemini 首页并优先抓取令牌。
- `token` 作为手动备用令牌，若自动抓取失败或页面内容变化时可直接使用。
- `token` 内容通常长度较长，并以 `SNlM0e` 开头。
- `cookies` 和 `token` 均应写入 `config.json`，保存后程序会自动重载。

## 运行与调用示例

### 健康检查

```bash
curl -s "http://127.0.0.1:8080/api/telemetry"
```

### 获取模型列表

```bash
curl -s "http://127.0.0.1:8080/v1/models" \
  -H "Authorization: Bearer your-api-key-here"
```

### 非流式对话调用

```bash
curl -s "http://127.0.0.1:8080/v1/chat/completions" \
  -H "Authorization: Bearer your-api-key-here" \
  -H "Content-Type: application/json" \
  -H "X-Session-ID: demo-session" \
  -d '{
    "model": "gemini-3-flash",
    "stream": false,
    "messages": [
      {"role": "system", "content": "你是一个助手"},
      {"role": "user", "content": "请介绍一下自己。"}
    ]
  }'
```

### 流式 SSE 调用

```bash
curl -N "http://127.0.0.1:8080/v1/chat/completions" \
  -H "Authorization: Bearer your-api-key-here" \
  -H "Content-Type: application/json" \
  -H "X-Session-ID: demo-session" \
  -d '{
    "model": "gemini-3-flash",
    "stream": true,
    "messages": [
      {"role": "user", "content": "用三句话介绍你自己。"}
    ]
  }'
```

### 会话保持

- 建议固定 `X-Session-ID`，例如 `demo-session` 或你的业务会话 ID。
- 该服务会复用 Gemini 会话上下文，实现连续对话体验。

## 常见问题与排查

- 访问 Gemini 失败 / 超时：
  - 先配置 `proxy`，或检查系统代理 `HTTP_PROXY` / `HTTPS_PROXY`。
  - 如果依旧失败，确认 `gemini_home_url` 是否可访问。

- 返回 HTML、出现验证码、异常流量页面：
  - 尝试填写 `cookies`；
  - 或更换网络/代理 IP；
  - 检查 `log_level` 是否为 `debug`，查看具体请求响应。

- 对话不连续：
  - 请固定 `X-Session-ID`，不要每次都换；
  - 如果需要重置，上层换一个新的 `X-Session-ID` 即可。

- 自动 token 获取失败：
  - 可以手动设置 `config.json` 的 `token`；
  - 若依赖登录 Cookie，请确保 `cookies` 正确且未过期。

## 备注

- `tokens` 字段当前为预留字段，暂未被业务逻辑使用。
- `gemini_url` / `gemini_home_url` 适用于你自己搭建的反代地址或特殊网络环境。
- 本服务主要用于本地/内网代理调用，不建议直接暴露到公网。
