# Gemini Web API

`geminiweb2api` 是一个用 Go 编写的轻量级代理服务：对外提供 OpenAI 兼容的 `/v1/chat/completions`，对内转发到 Gemini 网页端的流式后端；同时提供会话保持、简单日志与遥测。

```
+-----------------------------------------------------------------------------------+
| API 使用教程 (从配置到调用)                                                       |
+-----------------------------------------------------------------------------------+
| 0. 你需要的只有: 一个自定义 `api_key` (用于本地/内网鉴权)                           |
| 1. 编辑 `config.json` -> 填 `api_key`，需要时再填 `proxy` / `cookies`               |
| 2. 启动服务: `go run .`                                                           |
| 3. 健康检查: GET `http://127.0.0.1:8080/api/telemetry`                            |
| 4. 打开教程: GET `http://127.0.0.1:8080/help`                                     |
| 5. 调用接口: POST `http://127.0.0.1:8080/v1/chat/completions`                     |
|    - Header: `Authorization: Bearer <api_key>`                                    |
|    - 推荐额外加: `X-Session-ID: <stable-id>` 保持连续对话                          |
+-----------------------------------------------------------------------------------+
```

## 快速开始 (最常用的配置)

1) 修改 `config.json` (只改 `api_key` 也能跑起来)

```json
{
  "api_key": "change-me",
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
    "geminiweb2api running"
  ]
}
```

2) 启动

```bash
go mod tidy
go run .
```

3) 验证服务

```bash
curl -s "http://127.0.0.1:8080/api/telemetry"
curl -s "http://127.0.0.1:8080/v1/models"
```

4) 调用对话 (非流式)

```bash
curl -s "http://127.0.0.1:8080/v1/chat/completions" \
  -H "Authorization: Bearer change-me" \
  -H "Content-Type: application/json" \
  -H "X-Session-ID: demo-session" \
  -d "{\"model\":\"gemini-3-flash\",\"stream\":false,\"messages\":[{\"role\":\"user\",\"content\":\"你好\"}]}"
```

5) 调用对话 (流式 SSE)

```bash
curl -N "http://127.0.0.1:8080/v1/chat/completions" \
  -H "Authorization: Bearer change-me" \
  -H "Content-Type: application/json" \
  -H "X-Session-ID: demo-session" \
  -d "{\"model\":\"gemini-3-flash\",\"stream\":true,\"messages\":[{\"role\":\"user\",\"content\":\"用三句话介绍一下你自己\"}]}"
```

```
+----------------------------------+--------------------------------------------------+
| 常见问题                         | 快速定位                                         |
+----------------------------------+--------------------------------------------------+
| 访问 Gemini 失败/超时            | 先填 `proxy`，或设置系统代理 `HTTP_PROXY` 等       |
| 响应变成 HTML/出现人机识别/异常流量 | 更换 IP/代理；必要时填 `cookies` 提升稳定性         |
| 对话不连续                        | 固定 `X-Session-ID`，不要让它每次变                |
+----------------------------------+--------------------------------------------------+
```

## 核心能力

- OpenAI 兼容接口：`/v1/chat/completions` (流式/非流式)、`/v1/models`。
- 会话保持：推荐通过 `X-Session-ID` 固定会话键，后端会复用 Gemini 的 `c_*/r_*/rc_*` 上下文。
- 配置热加载：监控 `config.json`，每 5 秒检查一次文件变更并自动重载。
- 代理支持：`proxy` 优先，其次读环境变量 `HTTP_PROXY`/`HTTPS_PROXY`/`ALL_PROXY`。
- 遥测与监控：`/api/telemetry` 返回 uptime、RPM、token 统计与 `note`。
- 日志：`debug`/`info`/`warn`/`error`，可输出到控制台或文件。

## 详细配置说明 (`config.json`)

```
+-------------------+----------------+--------------------------------------------------+
| 字段              | 必填           | 说明                                             |
+-------------------+----------------+--------------------------------------------------+
| api_key           | 是             | 你自己设定的 Bearer key (用于保护本服务接口)      |
| port              | 否             | HTTP 监听端口，默认 8080                         |
| log_level         | 否             | debug/info/warn/error                            |
| log_file          | 否             | 为空 -> stdout；非空 -> 追加写入该文件             |
| note              | 否             | /api/telemetry 里展示的备注文本                   |
| proxy             | 否             | 显式代理地址 (如 http://127.0.0.1:7890)           |
| cookies           | 否 (推荐可填)  | Gemini 网页 Cookie；用于更稳定地抓取/刷新令牌     |
| token             | 否             | 手动填 SNlM0e；通常不建议，优先让程序自动抓        |
| tokens            | 否             | 预留字段 (当前代码未使用)                         |
| gemini_url        | 否             | 覆盖 StreamGenerate 地址 (反代/自建入口)          |
| gemini_home_url   | 否             | 覆盖 Gemini 首页地址 (配合反代抓取页面令牌)        |
+-------------------+----------------+--------------------------------------------------+
```

```
+-----------------------------------------------------------------------------------+
| 凭据安全提示                                                                      |
+-----------------------------------------------------------------------------------+
| - 不要把真实 `api_key` / `cookies` 提交到 git                                      |
| - 需要共享配置时，用环境变量/私有配置文件/CI Secret 管理                           |
+-----------------------------------------------------------------------------------+
```

### 关于 `cookies` / `token` (什么时候需要?)

- 默认情况下，服务会按会话自动去 `gemini_home_url` 抓取匿名 `SNlM0e`，尽量做到开箱即用。
- 如果你的网络环境经常触发风控 (返回 HTML、验证码、异常流量页面)，建议配置 `proxy`，必要时再提供已登录的 `cookies` 来提升稳定性。

## API 说明

### GET `/`

返回一个简单的 Web UI (HTML)。

### GET `/help`

返回一个可视化教程页 (HTML)：配置说明、调用示例、流式 SSE 说明与排障速查。

### GET `/api/telemetry`

返回服务遥测数据 (JSON)：运行时间、RPM、token 统计与 `note` 内容，便于健康检查。

### GET `/v1/models`

返回 OpenAI 风格的模型列表 (JSON)。

### POST `/v1/chat/completions`

请求要点：

- 必须包含 `Authorization: Bearer <api_key>`。
- 推荐固定 `X-Session-ID: <stable-id>`，用于保持连续对话上下文。
- `stream=true` 时返回 SSE (`chat.completion.chunk` + `data: [DONE]`)；否则返回一次性 `chat.completion`。

请求体示例：

```json
{
  "model": "gemini-3-flash",
  "stream": true,
  "messages": [
    {"role": "system", "content": "你是一个助手"},
    {"role": "user", "content": "你好"}
  ]
}
```

小技巧：

- 你也可以在 body 里传 `conversation_id`，服务会用它作为会话键来恢复/绑定上下文。

## 日志与指标

- 日志粒度由 `log_level` 控制；`debug` 会输出更完整的请求/响应片段，排障更方便。
- 控制台日志默认启用彩色等级前缀；如需关闭，设置环境变量 `NO_COLOR=1` 或 `CLICOLOR=0`。
- `/api/telemetry` 的统计包含：总请求/成功/失败、输入/输出 token (粗略估算) 与用于计算 RPM 的最近请求时间戳。

## 贡献

- 修改文档/代码后，按你自己的工作流提交即可 (此处不内置固定的 git 命令模板)。
