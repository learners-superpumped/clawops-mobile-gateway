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
	"time"
)

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

// enrollRequest 는 UI(/api/enroll)가 보내는 입력.
type enrollRequest struct {
	EnrollToken string `json:"enroll_token"`
	APIBase     string `json:"api_base"` // 선택: 미지정 시 CLAWOPS_API_BASE
	MSISDN      string `json:"msisdn"`   // 선택: 이 박스 휴대폰 번호(표시/발신 caller-id)
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

	// ① WG 키페어(개인키는 로컬에만).
	pubkey, err := s.sys.ensureWGKeypair()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "WireGuard 키 생성 실패: "+err.Error())
		return
	}

	// ② 콜홈 enroll.
	resp, status, err := callEnrollAPI(apiBase, req.EnrollToken, pubkey, req.MSISDN)
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

	// 터널/DID 값을 프로비저닝에 병합(asterisk 템플릿 렌더). BT 페어링 값은 그대로 둔다.
	p := s.cfg.Load()
	p.TunnelIP = resp.TunnelIP
	p.KamailioIP = resp.KamailioIP
	if req.MSISDN != "" {
		p.DID = req.MSISDN
	}
	rendered, cfgErr := s.cfg.Save(p)

	out := map[string]any{
		"ok":        upOK,
		"tunnel_ip": resp.TunnelIP,
		"wg_up":     upOK,
		"wg_output": upOut,
		"rendered":  rendered,
		"pubkey":    pubkey,
	}
	if cfgErr != nil {
		out["config_error"] = cfgErr.Error()
	}
	writeJSON(w, http.StatusOK, out)
}

// callEnrollAPI 는 ClawOps enroll 엔드포인트를 호출한다.
func callEnrollAPI(apiBase, token, pubkey, msisdn string) (enrollResponse, int, error) {
	body, _ := json.Marshal(map[string]string{"wgPubkey": pubkey, "msisdn": msisdn})
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
