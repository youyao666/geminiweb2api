package main

import (
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sync"
	"time"
)

type TokenInfo struct {
	SNlM0e    string
	BLToken   string
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

	patterns := []string{
		`"SNlM0e":"([^"]+)"`,
		`SNlM0e\\x22:\\x22([^\\]+)\\x22`,
		`WIZ_global_data[^}]*"SNlM0e":"([^"]+)"`,
		`SNlM0e\\":\\"([^\\]+)\\"`,
		`"SNlM0e"\s*:\s*"([^"]+)"`,
	}

	for _, pattern := range patterns {
		re := regexp.MustCompile(pattern)
		matches := re.FindSubmatch(body)
		if len(matches) > 1 {
			snlm0e := string(matches[1])
			if len(snlm0e) > 10 {
				logger.Debug("成功获取匿名 SNlM0e 令牌 (长度=%d)", len(snlm0e))
				return snlm0e, nil
			}
		}
	}

	return "", fmt.Errorf("SNlM0e not found in anonymous page")
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

	snlm0ePatterns := []string{
		`"SNlM0e":"([^"]+)"`,
		`SNlM0e\\x22:\\x22([^\\]+)\\x22`,
		`WIZ_global_data[^}]*"SNlM0e":"([^"]+)"`,
		`SNlM0e\\":\\"([^\\]+)\\"`,
		`"SNlM0e"\s*:\s*"([^"]+)"`,
	}

	blPatterns := []string{
		`"cfb2h":"([^"]+)"`,
		`cfb2h\\x22:\\x22([^\\]+)\\x22`,
		`"cfb2h"\s*:\s*"([^"]+)"`,
		`cfb2h\\":\\"([^\\]+)\\"`,
	}

	var snlm0e, blToken string
	for _, pattern := range snlm0ePatterns {
		re := regexp.MustCompile(pattern)
		matches := re.FindSubmatch(body)
		if len(matches) > 1 {
			token := string(matches[1])
			if len(token) > 10 {
				snlm0e = token
				break
			}
		}
	}

	for _, pattern := range blPatterns {
		re := regexp.MustCompile(pattern)
		matches := re.FindSubmatch(body)
		if len(matches) > 1 {
			blToken = string(matches[1])
			break
		}
	}

	if snlm0e != "" {
		tokenInfo.mutex.Lock()
		tokenInfo.SNlM0e = snlm0e
		if blToken != "" {
			tokenInfo.BLToken = blToken
		}
		tokenInfo.FetchedAt = time.Now()
		tokenInfo.mutex.Unlock()
		logger.Info("令牌获取成功: SNlM0e (长度=%d), BL=%s", len(snlm0e), blToken)
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

	return fmt.Errorf("SNlM0e token not found in page")
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
	needRefresh := tokenInfo.SNlM0e == "" || time.Since(tokenInfo.FetchedAt) > 30*time.Minute
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
