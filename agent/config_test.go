package main

import (
	"os"
	"path/filepath"
	"testing"
)

// Save 는 값이 다 차기 전엔 렌더하지 않는다 — 빈 값으로 쓰인 .conf 가 남으면
// clawops-asterisk 가 enable 되어 있어 재부팅 시 그 설정으로 기동을 시도한다.
// (state.json 은 부분 진행 보존을 위해 항상 저장한다.)
func TestSave_SkipsRenderUntilComplete(t *testing.T) {
	stateDir = t.TempDir()
	sysconfDir = t.TempDir()
	if err := os.WriteFile(filepath.Join(sysconfDir, "chan_mobile.conf.tmpl"), []byte("addr={{PHONE_MAC}}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	c := NewConfigManager()

	// 미완(HFP 포트·DID 없음) → 렌더 없음, 그러나 state 는 저장.
	rendered, err := c.Save(Provisioning{AdapterMAC: "AA:BB:CC:DD:EE:FF", PhoneMAC: "11:22:33:44:55:66", TunnelIP: "10.9.0.3", KamailioIP: "10.0.1.3"})
	if err != nil {
		t.Fatal(err)
	}
	if len(rendered) != 0 {
		t.Errorf("미완인데 렌더됨: %v", rendered)
	}
	if _, err := os.Stat(filepath.Join(sysconfDir, "chan_mobile.conf")); !os.IsNotExist(err) {
		t.Error("미완인데 .conf 가 생성됨")
	}
	if c.Load().PhoneMAC != "11:22:33:44:55:66" {
		t.Error("부분 진행이 state 에 보존되지 않음")
	}

	// 값이 다 차면 렌더.
	if _, err := c.Save(Provisioning{AdapterMAC: "AA:BB:CC:DD:EE:FF", PhoneMAC: "11:22:33:44:55:66", HFPPort: 8, TunnelIP: "10.9.0.3", KamailioIP: "10.0.1.3", DID: "01012345678"}); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(sysconfDir, "chan_mobile.conf"))
	if err != nil || string(b) != "addr=11:22:33:44:55:66\n" {
		t.Errorf("완성인데 렌더 안 됨: %q err=%v", string(b), err)
	}
}
