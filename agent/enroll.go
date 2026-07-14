package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// enroll 을 직렬화한다 — 동시 콜홈(더블클릭 등)이 키페어를 두 번 생성해 서버에 등록한 공개키와
// wg0.conf 에 쓰인 개인키가 어긋나(핸드셰이크 불가) 나는 레이스를 막는다.
var enrollMu sync.Mutex

// enroll: 박스가 ClawOps 에 셀프 온보딩하는 콜홈 흐름.
//
//  ① WireGuard 키페어를 박스에서 로컬 생성한다 — 개인키는 박스 밖으로 절대 나가지 않는다(공개키만 전송).
//  ② 운영자가 콘솔에서 발급받아 붙여넣은 1회용 enroll 토큰으로 ClawOps enroll API 를 호출한다.
//  ③ 응답으로 받은 터널 설정(할당된 터널 IP·게이트웨이 endpoint/pubkey·SIP/RTP 대상)으로 wg0.conf 를
//     조립하고 터널을 올린다. ← 내부 주소는 전부 이 응답에서 온다(소스에 박제하지 않는다).
//
// 이렇게 하면 이 공개 레포엔 어떤 내부 IP·엔드포인트도 담기지 않는다.

var (
	wgDir       = envOr("CLAWOPS_WG_DIR", "/etc/wireguard")
	wgInterface = envOr("CLAWOPS_WG_IFACE", "wg0")
	// 콘솔이 주는 API base(공개). 요청 body 로도 오버라이드 가능. 소스 기본값은 비워둔다(내부주소 미포함).
	defaultAPIBase = os.Getenv("CLAWOPS_API_BASE")
)

func wgPrivKeyPath() string { return filepath.Join(wgDir, "box.key") }
func wgConfPath() string    { return filepath.Join(wgDir, wgInterface+".conf") }

// enrollRequest 는 UI(/api/enroll)가 보내는 입력. UI 는 토큰만 보낸다 —
// 번호는 3단계 번호 검증(MO SMS)이 확정하므로 여기서 받지 않는다.
type enrollRequest struct {
	EnrollToken string `json:"enroll_token"`
	APIBase     string `json:"api_base"` // 선택: 미지정 시 CLAWOPS_API_BASE(개발/스테이징용, UI 미노출)
}

// enrollResponse 는 ClawOps enroll API 응답. 내부 주소는 전부 여기서 온다.
type enrollResponse struct {
	TunnelIP     string `json:"tunnelIp"`
	TunnelCIDR   string `json:"tunnelCidr"`
	KamailioIP   string `json:"kamailioIp"`
	RTPEngineIP  string `json:"rtpengineIp"`
	RTPPortRange string `json:"rtpPortRange"`
	WGGwEndpoint string `json:"wgGwEndpoint"`
	WGGwPubkey   string `json:"wgGwPubkey"`
	Keepalive    int    `json:"keepalive"`
	// 번호 소유 검증 안내(Phase 4). 박스는 nonce 를 사용자에게 보여주고, 사용자가 페어링된 폰으로
	// receiveNumber 에 이 코드를 문자로 보내면 서버가 발신 CLI 를 검증 번호로 확정한다.
	Verification struct {
		Nonce         string `json:"nonce"`
		ReceiveNumber string `json:"receiveNumber"`
	} `json:"verification"`
}

// ── /api/enroll ──────────────────────────────────────────────────

func (s *Server) handleEnroll(w http.ResponseWriter, r *http.Request) {
	var req enrollRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "잘못된 JSON")
		return
	}
	req.EnrollToken = strings.TrimSpace(req.EnrollToken)
	if req.EnrollToken == "" {
		writeErr(w, http.StatusBadRequest, "enroll_token 이 필요합니다")
		return
	}
	apiBase := strings.TrimRight(strings.TrimSpace(req.APIBase), "/")
	if apiBase == "" {
		apiBase = strings.TrimRight(defaultAPIBase, "/")
	}
	if apiBase == "" {
		writeErr(w, http.StatusBadRequest, "api_base 가 필요합니다(콘솔에서 안내된 주소)")
		return
	}
	// enroll 토큰(Bearer)을 평문 HTTP 로 흘리지 않도록 https 강제.
	if !strings.HasPrefix(apiBase, "https://") {
		writeErr(w, http.StatusBadRequest, "api_base 는 https:// 여야 합니다")
		return
	}

	// 동시 enroll 직렬화(키페어 레이스 방지).
	enrollMu.Lock()
	defer enrollMu.Unlock()

	// ① WG 키페어(개인키는 로컬에만).
	pubkey, err := s.sys.ensureWGKeypair()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "WireGuard 키 생성 실패: "+err.Error())
		return
	}

	// ② 콜홈 enroll.
	resp, status, err := callEnrollAPI(apiBase, req.EnrollToken, pubkey)
	if err != nil {
		writeErr(w, http.StatusBadGateway, "enroll 호출 실패: "+err.Error())
		return
	}
	if status != http.StatusCreated {
		// cpaas-api 의 에러(401 토큰무효·409 중복 등)를 그대로 전달.
		writeErr(w, status, "enroll 거절("+itoa(status)+")")
		return
	}

	// ③ wg0.conf 조립 + 터널 기동.
	if err := s.sys.writeWGConf(resp); err != nil {
		writeErr(w, http.StatusInternalServerError, "wg0.conf 작성 실패: "+err.Error())
		return
	}
	upOut, upOK := s.sys.wgUp()

	// 터널 값을 프로비저닝에 병합(asterisk 템플릿 렌더). BT 페어링 값은 그대로 둔다.
	// DID 는 여기서 채우지 않는다 — 번호 소유 검증(Phase 4) 통과 시 서버가 확정한 번호로만 주입한다
	// (msisdn 은 관측/표시용이라 신뢰 X). 검증 전엔 did 미설정 = provisioned=false = 서비스 시작 잠금.
	p := s.cfg.Load()
	p.TunnelIP = resp.TunnelIP
	p.KamailioIP = resp.KamailioIP
	rendered, cfgErr := s.cfg.Save(p)

	// 검증 세션 저장(재로드 후에도 안내·폴링 유지). 이전 게이트웨이의 검증 캐시는 새 nonce 로 리셋.
	if resp.Verification.Nonce != "" {
		_ = s.cfg.SaveVerification(VerificationState{
			Nonce:         resp.Verification.Nonce,
			ReceiveNumber: resp.Verification.ReceiveNumber,
			APIBase:       apiBase,
		})
	}

	enrolled := upOK && cfgErr == nil
	out := map[string]any{
		"ok":        enrolled,
		"tunnel_ip": resp.TunnelIP,
		"wg_up":     upOK,
		"wg_output": upOut,
		"rendered":  rendered,
		"pubkey":    pubkey,
		"verification": map[string]string{
			"nonce":          resp.Verification.Nonce,
			"receive_number": resp.Verification.ReceiveNumber,
		},
	}
	if cfgErr != nil {
		out["config_error"] = cfgErr.Error()
	}
	// 부분 실패(터널 미기동/템플릿 렌더 실패)를 200 으로 감추지 않는다 — 상태코드로도 신호.
	respStatus := http.StatusOK
	if !enrolled {
		respStatus = http.StatusBadGateway
	}
	writeJSON(w, respStatus, out)
}

// verificationStatusResp 는 ClawOps 검증상태 폴링 응답(GET /v1/mobile-gateways/verification/status).
type verificationStatusResp struct {
	Status         string `json:"status"` // pending | verified | expired | quota_exceeded
	VerifiedNumber string `json:"verifiedNumber"`
	ReceiveNumber  string `json:"receiveNumber"`
	ExpiresAt      string `json:"expiresAt"`
}

// pollVerificationStatus 는 nonce 로 검증 세션 상태를 조회한다. 404 는 세션 없음(만료/무효)으로
// err 아니라 status 로 전달. nonce 는 시크릿이라 이 조회 자체가 인증 — Authorization: Bearer 로 보낸다
// (쿼리스트링 금지: URL 은 로그·프록시에 남아 nonce 유출 시 번호 하이재킹 벡터가 된다).
func pollVerificationStatus(apiBase, nonce string) (verificationStatusResp, int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiBase+"/v1/mobile-gateways/verification/status", nil)
	if err != nil {
		return verificationStatusResp{}, 0, err
	}
	req.Header.Set("Authorization", "Bearer "+nonce)
	hr, err := http.DefaultClient.Do(req)
	if err != nil {
		return verificationStatusResp{}, 0, err
	}
	defer hr.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(hr.Body, 1<<20))
	if hr.StatusCode != http.StatusOK {
		return verificationStatusResp{}, hr.StatusCode, nil
	}
	var vs verificationStatusResp
	if err := json.Unmarshal(raw, &vs); err != nil {
		return verificationStatusResp{}, hr.StatusCode, fmt.Errorf("검증상태 파싱 실패: %w", err)
	}
	return vs, hr.StatusCode, nil
}

// callEnrollAPI 는 ClawOps enroll 엔드포인트를 호출한다.
func callEnrollAPI(apiBase, token, pubkey string) (enrollResponse, int, error) {
	body, _ := json.Marshal(map[string]string{"wgPubkey": pubkey})
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, apiBase+"/v1/mobile-gateways/enroll", bytes.NewReader(body))
	if err != nil {
		return enrollResponse{}, 0, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+token)
	hr, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return enrollResponse{}, 0, err
	}
	defer hr.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(hr.Body, 1<<20))
	if hr.StatusCode != http.StatusCreated {
		return enrollResponse{}, hr.StatusCode, nil
	}
	var er enrollResponse
	if err := json.Unmarshal(raw, &er); err != nil {
		return enrollResponse{}, hr.StatusCode, fmt.Errorf("응답 파싱 실패: %w", err)
	}
	if er.TunnelIP == "" || er.WGGwPubkey == "" || er.WGGwEndpoint == "" || er.KamailioIP == "" || er.RTPEngineIP == "" {
		return enrollResponse{}, hr.StatusCode, fmt.Errorf("응답에 필수 터널 설정 누락")
	}
	return er, hr.StatusCode, nil
}
