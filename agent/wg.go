package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// WireGuard 키/설정/기동 — enroll 흐름(enroll.go)이 사용한다.
// 개인키는 박스에서만 생성·보관하고 밖으로 내보내지 않는다(공개키만 enroll 로 전송).

// runStdin 은 stdin 을 넣어 명령을 실행한다(`wg pubkey` 가 개인키를 stdin 으로 받음).
func runStdin(input string, timeout time.Duration, name string, args ...string) (string, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdin = strings.NewReader(input)
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err == nil
}

// ensureWGKeypair 는 개인키(box.key, 0600)가 없으면 생성하고 공개키를 돌려준다(멱등).
// 재enroll 시 같은 키를 재사용한다 — 개인키는 절대 파일 밖으로 안 나간다.
func (s *System) ensureWGKeypair() (pubkey string, err error) {
	priv, rerr := os.ReadFile(wgPrivKeyPath())
	// 파일 부재뿐 아니라 빈/공백 키(중단된 WriteFile·operator touch)도 재생성해 복구한다 —
	// 안 그러면 0바이트 box.key 가 `wg pubkey` 빈 stdin 실패로 enroll 을 영구 브릭시킨다.
	if rerr != nil || len(strings.TrimSpace(string(priv))) == 0 {
		gen, ok := run(5*time.Second, "wg", "genkey")
		if !ok || gen == "" {
			return "", fmt.Errorf("wg genkey 실패: %s", gen)
		}
		if err := os.MkdirAll(wgDir, 0o700); err != nil {
			return "", err
		}
		if err := os.WriteFile(wgPrivKeyPath(), []byte(gen+"\n"), 0o600); err != nil {
			return "", err
		}
		priv = []byte(gen)
	}
	pub, ok := runStdin(strings.TrimSpace(string(priv)), 5*time.Second, "wg", "pubkey")
	if !ok || pub == "" {
		return "", fmt.Errorf("wg pubkey 실패")
	}
	return strings.TrimSpace(pub), nil
}

// writeWGConf 는 enroll 응답으로 wg0.conf 를 조립한다. AllowedIPs = SIP(kamailio)·RTP(rtpengine)
// 대상 /32 만 — 그 외 목적지는 터널에 실리지 않는다(클라측 1차 격리). 값은 전부 응답에서 온다.
func (s *System) writeWGConf(r enrollResponse) error {
	priv, err := os.ReadFile(wgPrivKeyPath())
	if err != nil {
		return fmt.Errorf("개인키 읽기: %w", err)
	}
	keepalive := r.Keepalive
	if keepalive <= 0 {
		keepalive = 25
	}
	conf := fmt.Sprintf(`[Interface]
PrivateKey = %s
Address = %s/32

[Peer]
PublicKey = %s
Endpoint = %s
AllowedIPs = %s/32, %s/32
PersistentKeepalive = %d
`, strings.TrimSpace(string(priv)), r.TunnelIP, r.WGGwPubkey, r.WGGwEndpoint, r.KamailioIP, r.RTPEngineIP, keepalive)

	if err := os.MkdirAll(wgDir, 0o700); err != nil {
		return err
	}
	return os.WriteFile(wgConfPath(), []byte(conf), 0o600)
}

// wgUp 은 wg-quick@<iface> 를 (재)기동해 새 conf 를 반영하고 재부팅 생존을 위해 enable 한다.
func (s *System) wgUp() (string, bool) {
	unit := "wg-quick@" + wgInterface
	run(10*time.Second, "systemctl", "enable", unit) // best-effort(재부팅 생존)
	return run(20*time.Second, "systemctl", "restart", unit)
}
