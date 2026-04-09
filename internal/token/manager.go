package token

import (
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"main/internal/config"
	"main/internal/httpclient"
	"main/internal/logging"
	"main/internal/support"
)

type TokenInfo struct {
	SNlM0e    string
	BLToken   string
	FSID      string
	ReqID     int64
	FetchedAt time.Time
	mutex     sync.RWMutex
}

type AnonToken struct {
	SNlM0e    string
	FetchedAt time.Time
	IsBad     bool
}

type pageState struct {
	RequestToken string
	BLToken      string
	FSID         string
}

type Manager struct {
	getConfig func() config.Config
	getClient func() *http.Client
	getLogger func() *logging.Logger

	tokenInfo *TokenInfo

	mutex         sync.RWMutex
	sessionTokens map[string]*AnonToken
}

func NewManager(getConfig func() config.Config, getClient func() *http.Client, getLogger func() *logging.Logger) *Manager {
	return &Manager{
		getConfig:     getConfig,
		getClient:     getClient,
		getLogger:     getLogger,
		tokenInfo:     &TokenInfo{},
		sessionTokens: make(map[string]*AnonToken),
	}
}

func (m *Manager) StartRefresher() {
	cfg := m.getConfig()
	if cfg.Cookies != "" {
		if err := m.fetchToken(); err != nil {
			m.getLogger().Warn("初始令牌获取失败: %v", err)
		}
	}

	go func() {
		ticker := time.NewTicker(25 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			m.RefreshTokenIfNeeded()
		}
	}()
}

func (m *Manager) RefreshTokenIfNeeded() {
	m.tokenInfo.mutex.RLock()
	needRefresh := m.tokenInfo.SNlM0e == "" ||
		m.tokenInfo.BLToken == "" ||
		m.tokenInfo.FSID == "" ||
		time.Since(m.tokenInfo.FetchedAt) > 30*time.Minute
	m.tokenInfo.mutex.RUnlock()

	cfg := m.getConfig()
	if needRefresh && cfg.Cookies != "" {
		if err := m.fetchToken(); err != nil {
			m.getLogger().Warn("自动刷新令牌失败: %v", err)
		}
	}
}

func (m *Manager) FetchAnonymousToken() (string, error) {
	endpoints := httpclient.CurrentGeminiEndpoints(m.getConfig())
	req, err := http.NewRequest("GET", endpoints.Home, nil)
	if err != nil {
		return "", fmt.Errorf("create request failed: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/128.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/webp,*/*;q=0.8")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")

	randomIP := support.GenerateRandomIP()
	req.Header.Set("X-Forwarded-For", randomIP)
	req.Header.Set("X-Real-IP", randomIP)

	resp, err := m.getClient().Do(req)
	if err != nil {
		return "", fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read body failed: %w", err)
	}

	state := extractPageState(body)
	m.updateTokenInfoFromState(state)
	if state.RequestToken == "" {
		return "", fmt.Errorf("request token not found in anonymous page")
	}

	m.getLogger().Debug("成功获取匿名请求令牌 (长度=%d)", len(state.RequestToken))
	return state.RequestToken, nil
}

func (m *Manager) GetTokenForSession(sessionKey string, isNewSession bool) (string, int) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	if token, exists := m.sessionTokens[sessionKey]; exists && !isNewSession && !token.IsBad {
		if time.Since(token.FetchedAt) < 25*time.Minute {
			return token.SNlM0e, 0
		}
	}

	snlm0e, err := m.FetchAnonymousToken()
	if err != nil {
		if token, exists := m.sessionTokens[sessionKey]; exists {
			return token.SNlM0e, 0
		}
		return "", 0
	}

	m.sessionTokens[sessionKey] = &AnonToken{
		SNlM0e:    snlm0e,
		FetchedAt: time.Now(),
	}
	m.getLogger().Debug("已为会话 %s 分配新的匿名令牌", sessionKey)
	return snlm0e, 0
}

func (m *Manager) MarkSessionTokenBad(sessionKey string) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	if token, exists := m.sessionTokens[sessionKey]; exists {
		token.IsBad = true
		m.getLogger().Warn("会话 %s 的令牌已被标记为失效", sessionKey)
	}
}

func (m *Manager) GetToken() string {
	m.tokenInfo.mutex.RLock()
	defer m.tokenInfo.mutex.RUnlock()

	if m.tokenInfo.SNlM0e != "" {
		return m.tokenInfo.SNlM0e
	}
	return m.getConfig().Token
}

func (m *Manager) GetBLToken() string {
	m.tokenInfo.mutex.RLock()
	defer m.tokenInfo.mutex.RUnlock()
	return m.tokenInfo.BLToken
}

func (m *Manager) GetFSID() string {
	m.tokenInfo.mutex.RLock()
	defer m.tokenInfo.mutex.RUnlock()
	return m.tokenInfo.FSID
}

func (m *Manager) NextReqID() string {
	m.tokenInfo.mutex.Lock()
	defer m.tokenInfo.mutex.Unlock()

	if m.tokenInfo.ReqID == 0 {
		m.tokenInfo.ReqID = seedReqID()
	}
	current := m.tokenInfo.ReqID
	m.tokenInfo.ReqID += 100000
	return strconv.FormatInt(current, 10)
}

func (m *Manager) fetchToken() error {
	cfg := m.getConfig()
	if cfg.Cookies == "" {
		return nil
	}

	endpoints := httpclient.CurrentGeminiEndpoints(cfg)
	req, err := http.NewRequest("GET", endpoints.Home, nil)
	if err != nil {
		return fmt.Errorf("create request failed: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/128.0.0.0 Safari/537.36")
	req.Header.Set("Cookie", cfg.Cookies)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/webp,*/*;q=0.8")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")
	req.Header.Set("X-Forwarded-For", support.GenerateRandomIP())

	resp, err := m.getClient().Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read body failed: %w", err)
	}

	state := extractPageState(body)
	m.updateTokenInfoFromState(state)

	if state.RequestToken != "" {
		m.getLogger().Info("页面态获取成功: token长度=%d, BL=%s, f.sid=%s", len(state.RequestToken), state.BLToken, state.FSID)
		return nil
	}

	if cfg.Token != "" {
		m.tokenInfo.mutex.Lock()
		m.tokenInfo.SNlM0e = cfg.Token
		m.tokenInfo.FetchedAt = time.Now()
		m.tokenInfo.mutex.Unlock()
		m.getLogger().Info("正在使用配置文件中的令牌")
		return nil
	}

	return fmt.Errorf("request token not found in page")
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

func (m *Manager) updateTokenInfoFromState(state pageState) {
	if state.RequestToken == "" && state.BLToken == "" && state.FSID == "" {
		return
	}

	m.tokenInfo.mutex.Lock()
	defer m.tokenInfo.mutex.Unlock()

	if state.RequestToken != "" {
		m.tokenInfo.SNlM0e = state.RequestToken
	}
	if state.BLToken != "" {
		m.tokenInfo.BLToken = state.BLToken
	}
	if state.FSID != "" {
		m.tokenInfo.FSID = state.FSID
	}
	if m.tokenInfo.ReqID == 0 {
		m.tokenInfo.ReqID = seedReqID()
	}
	m.tokenInfo.FetchedAt = time.Now()
}

func seedReqID() int64 {
	base := time.Now().UnixNano()%9000000 + 1000000
	if base%2 == 0 {
		base++
	}
	return base
}
