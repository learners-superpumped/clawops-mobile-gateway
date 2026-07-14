package main

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// decodeJSONBody 는 본문이 없거나 깨져도 에러로 취급하지 않는다 — 초기화 요청의 옵션은
// 전부 선택이라 빈 본문이면 기본값으로 동작해야 한다.
func decodeJSONBody(r *http.Request, dst any) error {
	if r.Body == nil {
		return nil
	}
	return json.NewDecoder(r.Body).Decode(dst)
}

// handleReset 은 이 게이트웨이를 공장 초기 상태로 되돌린다.
//
// 왜 필요한가 — 박스를 다른 계정으로 옮기거나, ClawOps 콘솔에서 폐기된 뒤 새로 연결하거나,
// 잘못 설정해 처음부터 다시 하려면 로컬 상태를 지워야 한다. 이 경로가 없으면 사용자는
// SSH 로 들어가 파일을 지우는 수밖에 없다(설치형 어플라이언스에선 사실상 불가능).
//
// 지우는 것:
//   - 서비스 정지(asterisk) — 옛 설정으로 통화가 이어지지 않도록 먼저.
//   - 렌더된 .conf (chan_mobile/pjsip/extensions) — .tmpl 은 패키지 자산이므로 남긴다.
//   - state.json / verification.json — 프로비저닝·검증 세션.
//   - wg0 터널 down + wg0.conf.
//   - 페어링된 휴대폰(BlueZ) — 다음 사용자가 남의 폰을 물려받지 않도록.
//
// ⚠️ box.key(WireGuard 개인키)는 기본적으로 남긴다. 같은 키를 유지하면 ClawOps 가 이 박스를
//
//	같은 게이트웨이로 알아보고 폐기 후 재등록 시 같은 터널 IP 로 재활성한다. 키를 지우면
//	새 게이트웨이로 등록돼 서버에 고아 행이 남는다(관리자가 폐기해야 함).
//	완전 초기화가 필요하면 {"wipe_identity": true}.
func (s *Server) handleReset(w http.ResponseWriter, r *http.Request) {
	var body struct {
		WipeIdentity bool `json:"wipe_identity"`
	}
	// 본문은 선택 — 없으면 기본(신원 유지) 초기화.
	_ = decodeJSONBody(r, &body)

	var steps []string

	// ① 서비스 먼저 정지 — 옛 설정으로 통화가 이어지면 안 된다.
	if out, ok := s.sys.ServiceAction("stop"); ok {
		steps = append(steps, "서비스 정지")
	} else if out != "" {
		steps = append(steps, "서비스 정지 실패: "+out)
	}

	// ② 렌더된 .conf 제거(.tmpl 은 패키지 자산이라 유지).
	if entries, err := os.ReadDir(sysconfDir); err == nil {
		for _, e := range entries {
			name := e.Name()
			switch name {
			case "chan_mobile.conf", "pjsip.conf", "extensions.conf":
				if err := os.Remove(filepath.Join(sysconfDir, name)); err == nil {
					steps = append(steps, "설정 삭제: "+name)
				}
			}
		}
	}

	// ③ 페어링된 휴대폰 제거 — 남의 폰이 남아 있으면 다음 온보딩이 그걸 물려받는다
	//    (실기에서 옛 iPhone 때문에 페어링 단계가 잘못 통과됐다).
	for _, d := range s.sys.btDevices(true) {
		if _, ok := run(8*time.Second, "bluetoothctl", "remove", d.MAC); ok {
			steps = append(steps, "페어링 해제: "+d.MAC)
		}
	}

	// ④ 터널 내리기 + conf 제거.
	if _, ok := run(15*time.Second, "systemctl", "stop", "wg-quick@"+wgInterface); ok {
		steps = append(steps, "터널 정지")
	}
	run(10*time.Second, "systemctl", "disable", "wg-quick@"+wgInterface)
	if err := os.Remove(wgConfPath()); err == nil {
		steps = append(steps, "터널 설정 삭제")
	}

	// ⑤ 로컬 상태(프로비저닝·검증 세션).
	if err := os.Remove(filepath.Join(stateDir, "state.json")); err == nil {
		steps = append(steps, "장치 설정 삭제")
	}
	if err := os.Remove(filepath.Join(stateDir, "verification.json")); err == nil {
		steps = append(steps, "검증 세션 삭제")
	}

	// ⑥ 신원(개인키) — 요청했을 때만.
	if body.WipeIdentity {
		if err := os.Remove(wgPrivKeyPath()); err == nil {
			steps = append(steps, "장치 키 삭제(새 게이트웨이로 등록됨)")
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":            true,
		"steps":         steps,
		"wipe_identity": body.WipeIdentity,
	})
}
