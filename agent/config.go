package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

// 경로(테스트/개발용으로 env 오버라이드 가능).
var (
	sysconfDir = envOr("CLAWOPS_SYSCONF", "/etc/clawops-gw/asterisk")
	stateDir   = envOr("CLAWOPS_STATE", "/var/lib/clawops-gw/agent")
)

// Provisioning 은 설정 템플릿의 {{PLACEHOLDER}} 를 채우는 값이다.
type Provisioning struct {
	AdapterMAC string `json:"adapter_mac"` // {{ADAPTER_MAC}}
	PhoneMAC   string `json:"phone_mac"`   // {{PHONE_MAC}}
	HFPPort    int    `json:"hfp_port"`    // {{HFP_PORT}}
	TunnelIP   string `json:"tunnel_ip"`   // {{TUNNEL_IP}}
	KamailioIP string `json:"kamailio_ip"` // {{KAMAILIO_IP}}
	DID        string `json:"did"`         // {{MY_DID}}
}

func (p Provisioning) placeholders() map[string]string {
	return map[string]string{
		"ADAPTER_MAC": p.AdapterMAC,
		"PHONE_MAC":   p.PhoneMAC,
		"HFP_PORT":    strconv.Itoa(p.HFPPort),
		"TUNNEL_IP":   p.TunnelIP,
		"KAMAILIO_IP": p.KamailioIP,
		"MY_DID":      p.DID,
	}
}

// filled 는 아직 안 채워진 필드명들을 돌려준다(빈 값·0 포트).
func (p Provisioning) missing() []string {
	var m []string
	for k, v := range map[string]string{
		"adapter_mac": p.AdapterMAC, "phone_mac": p.PhoneMAC,
		"tunnel_ip": p.TunnelIP, "kamailio_ip": p.KamailioIP, "did": p.DID,
	} {
		if strings.TrimSpace(v) == "" {
			m = append(m, k)
		}
	}
	if p.HFPPort == 0 {
		m = append(m, "hfp_port")
	}
	return m
}

// ConfigManager 는 프로비저닝 값을 저장/로드하고 템플릿을 렌더한다.
type ConfigManager struct {
	mu sync.Mutex
}

func NewConfigManager() *ConfigManager { return &ConfigManager{} }

func (c *ConfigManager) statePath() string { return filepath.Join(stateDir, "state.json") }

// Load 는 저장된 프로비저닝 값을 읽는다(없으면 빈 값).
func (c *ConfigManager) Load() Provisioning {
	c.mu.Lock()
	defer c.mu.Unlock()
	var p Provisioning
	if b, err := os.ReadFile(c.statePath()); err == nil {
		_ = json.Unmarshal(b, &p)
	}
	return p
}

// Save 는 값을 저장하고 템플릿을 렌더(.tmpl → .conf)한다. 렌더된 .conf 목록을 돌려준다.
func (c *ConfigManager) Save(p Provisioning) ([]string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		return nil, err
	}
	b, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(c.statePath(), b, 0o600); err != nil {
		return nil, err
	}
	return renderTemplates(sysconfDir, p.placeholders())
}

// renderTemplates 는 dir 안의 모든 *.tmpl 을 치환 후 .tmpl 을 뗀 .conf 로 쓴다.
// 예: chan_mobile.conf.tmpl → chan_mobile.conf
func renderTemplates(dir string, vals map[string]string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var written []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".tmpl") {
			continue
		}
		src := filepath.Join(dir, e.Name())
		raw, err := os.ReadFile(src)
		if err != nil {
			return written, err
		}
		out := string(raw)
		for k, v := range vals {
			out = strings.ReplaceAll(out, "{{"+k+"}}", v)
		}
		dst := filepath.Join(dir, strings.TrimSuffix(e.Name(), ".tmpl"))
		if err := os.WriteFile(dst, []byte(out), 0o640); err != nil {
			return written, err
		}
		written = append(written, filepath.Base(dst))
	}
	return written, nil
}

// itoa 는 system.go 에서 쓰는 작은 헬퍼.
func itoa(n int) string { return strconv.Itoa(n) }
