package main

import (
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

type TokenInfo struct {
	SNlM0e    string
	BLToken   string
	FSID      string
	ReqID     int64
	FetchedAt time.Time
	mutex     sync.RWMutex
}

var tokenInfo = &TokenInfo{}

type AnonToken struct {
	SNlM0e    string
	FetchedAt time.Time
	IsBad     bool
}

type TokenManager struct {
	sessionTokens map[string]*AnonToken
	mutex         sync.RWMutex
}

var tokenManager = &TokenManager{
	sessionTokens: make(map[string]*AnonToken),
}

type pageState struct {
	RequestToken string
	BLToken      string
	FSID         string
}

func (tm *TokenManager) Init() {}

func (tm *TokenManager) FetchAnonymousToken() (string, error) {
	endpoints := currentGeminiEndpoints()
	req, err := http.NewRequest("GET", endpoints.home, nil)
	if err != nil {
		return "", fmt.Errorf("create request failed: %v", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/128.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/webp,*/*;q=0.8")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")
	randomIP := generateRandomIP()
	req.Header.Set("X-Forwarded-For", randomIP)
	req.Header.Set("X-Real-IP", randomIP)

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read body failed: %v", err)
	}

	state := extractPageState(body)
	updateTokenInfoFromState(state)
	if state.RequestToken != "" {
		logger.Debug("成功获取匿名请求令牌 (长度=%d)", len(state.RequestToken))
		return state.RequestToken, nil
	}

	return "", fmt.Errorf("request token not found in anonymous page")
}

func (tm *TokenManager) GetTokenForSession(sessionKey string, isNewSession bool) (string, int) {
	tm.mutex.Lock()
	defer tm.mutex.Unlock()

	if token, exists := tm.sessionTokens[sessionKey]; exists && !isNewSession && !token.IsBad {
		if time.Since(token.FetchedAt) < 25*time.Minute {
			return token.SNlM0e, 0
		}
	}
	snlm0e, err := tm.FetchAnonymousToken()
	if err != nil {
		if token, exists := tm.sessionTokens[sessionKey]; exists {
			return token.SNlM0e, 0
		}
		return "", 0
	}

	tm.sessionTokens[sessionKey] = &AnonToken{
		SNlM0e:    snlm0e,
		FetchedAt: time.Now(),
		IsBad:     false,
	}
	logger.Debug("已为会话 %s 分配新的匿名令牌", sessionKey)
	return snlm0e, 0
}

func (tm *TokenManager) MarkTokenBad(idx int) (string, int) {
	return "", 0
}

func (tm *TokenManager) MarkSessionTokenBad(sessionKey string) {
	tm.mutex.Lock()
	defer tm.mutex.Unlock()

	if token, exists := tm.sessionTokens[sessionKey]; exists {
		token.IsBad = true
		logger.Warn("会话 %s 的令牌已被标记为失效", sessionKey)
	}
}

func fetchToken() error {
	cfg := getConfigSnapshot()
	if cfg.Cookies == "" {
		return nil
	}

	endpoints := currentGeminiEndpoints()
	req, err := http.NewRequest("GET", endpoints.home, nil)
	if err != nil {
		return fmt.Errorf("create request failed: %v", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/128.0.0.0 Safari/537.36")
	req.Header.Set("Cookie", cfg.Cookies)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/webp,*/*;q=0.8")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")
	randomIP := generateRandomIP()
	req.Header.Set("X-Forwarded-For", randomIP)

	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read body failed: %v", err)
	}

	state := extractPageState(body)
	updateTokenInfoFromState(state)

	if state.RequestToken != "" {
		logger.Info("页面态获取成功: token长度=%d, BL=%s, f.sid=%s", len(state.RequestToken), state.BLToken, state.FSID)
		return nil
	}

	if config.Token != "" {
		tokenInfo.mutex.Lock()
		tokenInfo.SNlM0e = config.Token
		tokenInfo.FetchedAt = time.Now()
		tokenInfo.mutex.Unlock()
		logger.Info("正在使用配置文件中的令牌")
		return nil
	}

	return fmt.Errorf("request token not found in page")
}

func getToken() string {
	tokenInfo.mutex.RLock()
	defer tokenInfo.mutex.RUnlock()
	if tokenInfo.SNlM0e != "" {
		return tokenInfo.SNlM0e
	}
	cfg := getConfigSnapshot()
	return cfg.Token
}

func refreshTokenIfNeeded() {
	tokenInfo.mutex.RLock()
	needRefresh := tokenInfo.SNlM0e == "" || tokenInfo.BLToken == "" || tokenInfo.FSID == "" || time.Since(tokenInfo.FetchedAt) > 30*time.Minute
	tokenInfo.mutex.RUnlock()

	cfg := getConfigSnapshot()
	if needRefresh && cfg.Cookies != "" {
		if err := fetchToken(); err != nil {
			logger.Warn("自动刷新令牌失败: %v", err)
		}
	}
}

func startTokenRefresher() {
	cfg := getConfigSnapshot()
	if cfg.Cookies != "" {
		if err := fetchToken(); err != nil {
			logger.Warn("初始令牌获取失败: %v", err)
		}
	}
	go func() {
		ticker := time.NewTicker(25 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			refreshTokenIfNeeded()
		}
	}()
}

func extractPageState(body []byte) pageState {
	return pageState{
		RequestToken: firstMatch(body, []string{
			`"SNlM0e":"([^"]+)"`,
			`SNlM0e\\x22:\\x22([^\\]+)\\x22`,
			`WIZ_global_data[^}]*"SNlM0e":"([^"]+)"`,
			`SNlM0e\\":\\"([^\\]+)\\"`,
			`"SNlM0e"\s*:\s*"([^"]+)"`,
		}),
		BLToken: firstMatch(body, []string{
			`"cfb2h":"([^"]+)"`,
			`cfb2h\\x22:\\x22([^\\]+)\\x22`,
			`"cfb2h"\s*:\s*"([^"]+)"`,
			`cfb2h\\":\\"([^\\]+)\\"`,
		}),
		FSID: firstMatch(body, []string{
			`[?&]f\.sid=([-0-9]+)`,
			`"f\.sid"\s*:\s*"?(?:\\u003d)?([-0-9]+)"?`,
			`"f\.sid":"([-0-9]+)"`,
			`f\.sid\\":\\"([-0-9]+)\\"`,
			`"FdrFJe":"([-0-9]+)"`,
			`FdrFJe\\x22:\\x22([-0-9]+)\\x22`,
		}),
	}
}

func firstMatch(body []byte, patterns []string) string {
	for _, pattern := range patterns {
		re := regexp.MustCompile(pattern)
		matches := re.FindSubmatch(body)
		if len(matches) > 1 {
			value := string(matches[1])
			if strings.TrimSpace(value) != "" {
				return value
			}
		}
	}
	return ""
}

func updateTokenInfoFromState(state pageState) {
	if state.RequestToken == "" && state.BLToken == "" && state.FSID == "" {
		return
	}

	tokenInfo.mutex.Lock()
	defer tokenInfo.mutex.Unlock()

	if state.RequestToken != "" {
		tokenInfo.SNlM0e = state.RequestToken
	}
	if state.BLToken != "" {
		tokenInfo.BLToken = state.BLToken
	}
	if state.FSID != "" {
		tokenInfo.FSID = state.FSID
	}
	if tokenInfo.ReqID == 0 {
		tokenInfo.ReqID = seedReqID()
	}
	tokenInfo.FetchedAt = time.Now()
}

func getBLToken() string {
	tokenInfo.mutex.RLock()
	defer tokenInfo.mutex.RUnlock()
	return tokenInfo.BLToken
}

func getFSID() string {
	tokenInfo.mutex.RLock()
	defer tokenInfo.mutex.RUnlock()
	return tokenInfo.FSID
}

func nextReqID() string {
	tokenInfo.mutex.Lock()
	defer tokenInfo.mutex.Unlock()

	if tokenInfo.ReqID == 0 {
		tokenInfo.ReqID = seedReqID()
	}
	current := tokenInfo.ReqID
	tokenInfo.ReqID += 100000
	return strconv.FormatInt(current, 10)
}

func seedReqID() int64 {
	base := time.Now().UnixNano()%9000000 + 1000000
	if base%2 == 0 {
		base++
	}
	return base
}
