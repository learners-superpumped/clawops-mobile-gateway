package main

import (
	"encoding/json"
	"net/http"
	"strconv"
)

type Server struct {
	sys *System
	cfg *ConfigManager
}

// ── /api/status ──────────────────────────────────────────────────

type StatusResponse struct {
	Service    ServiceStatus    `json:"service"`
	Bluetooth  BTStatus         `json:"bluetooth"`
	Tunnel     TunnelStatus     `json:"tunnel"`
	ChanMobile ChanMobileStatus `json:"chan_mobile"`
	Config     ConfigState      `json:"config"`
}

type ConfigState struct {
	Provisioning Provisioning `json:"provisioning"`
	Missing      []string     `json:"missing"`     // 아직 안 채운 필드
	Provisioned  bool         `json:"provisioned"` // 전부 채워짐?
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	p := s.cfg.Load()
	missing := p.missing()
	writeJSON(w, http.StatusOK, StatusResponse{
		Service:    s.sys.ServiceStatus(),
		Bluetooth:  s.sys.BTStatus(),
		Tunnel:     s.sys.TunnelStatus(),
		ChanMobile: s.sys.ChanMobileStatus(),
		Config: ConfigState{
			Provisioning: p,
			Missing:      missing,
			Provisioned:  len(missing) == 0,
		},
	})
}

// ── /api/verification (Phase 4) ──────────────────────────────────
// 저장된 nonce 로 ClawOps 검증 상태를 폴링해 UI 에 노출한다. verified 확인 시 검증 번호를 DID 로
// 1회 자동 주입하고 템플릿을 렌더 → 이후 서비스 시작 단계가 열린다(수동 DID 입력 제거).
// state: none(enroll 전) | pending | verified | expired | quota_exceeded | gone(세션 없음).

func (s *Server) handleVerification(w http.ResponseWriter, r *http.Request) {
	v := s.cfg.LoadVerification()
	if v.Nonce == "" {
		writeJSON(w, http.StatusOK, map[string]any{"state": "none"})
		return
	}
	base := map[string]any{"nonce": v.Nonce, "receive_number": v.ReceiveNumber}

	// 이미 verified 로 확정·DID 주입까지 끝났으면 서버 폴링 없이 즉시 반환.
	if v.VerifiedNumber != "" {
		base["state"] = "verified"
		base["verified_number"] = v.VerifiedNumber
		writeJSON(w, http.StatusOK, base)
		return
	}

	vs, code, err := pollVerificationStatus(v.APIBase, v.Nonce)
	if err != nil {
		// 일시적 폴링 실패는 pending 으로 표시(UI 폴링이 계속 재시도).
		base["state"] = "pending"
		base["poll_error"] = err.Error()
		writeJSON(w, http.StatusOK, base)
		return
	}
	if code == http.StatusNotFound {
		base["state"] = "gone"
		writeJSON(w, http.StatusOK, base)
		return
	}

	// verified → 검증 번호를 DID 로 1회 주입 + 템플릿 렌더(이후 서비스 시작 가능).
	if vs.Status == "verified" && vs.VerifiedNumber != "" {
		p := s.cfg.Load()
		p.DID = vs.VerifiedNumber
		if _, err := s.cfg.Save(p); err == nil {
			v.VerifiedNumber = vs.VerifiedNumber
			_ = s.cfg.SaveVerification(v)
		}
	}

	base["state"] = vs.Status
	base["verified_number"] = vs.VerifiedNumber
	base["expires_at"] = vs.ExpiresAt
	writeJSON(w, http.StatusOK, base)
}

// ── /api/bluetooth ───────────────────────────────────────────────

func (s *Server) handleBTList(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"devices": s.sys.btDevices(false)})
}

func (s *Server) handleBTScan(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"devices": s.sys.ScanDevices()})
}

func (s *Server) handleBTPair(w http.ResponseWriter, r *http.Request) {
	var body struct {
		MAC string `json:"mac"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.MAC == "" {
		writeErr(w, http.StatusBadRequest, "mac 필요")
		return
	}
	out, ok := s.sys.Pair(body.MAC)
	if !ok {
		writeJSON(w, statusFor(false), map[string]any{"ok": false, "output": out})
		return
	}

	// 페어링은 확정된 사실이므로 회선 정보를 여기서 확보해 즉시 저장한다.
	//  - 폼에만 채우고 사용자의 "저장"을 기다리면 새로고침 한 번에 값이 날아간다.
	//  - HFP 포트는 사용자가 알 수 없는 값이고(sdptool 조회), 폰이 가깝고 깨어 있는
	//    지금이 조회에 가장 유리한 시점이다. 나중에 4단계에서 시도하면 폰이 멀어지거나
	//    잠들어 "Host is down" 으로 실패한다.
	//  - 어댑터 MAC 은 이미 status 로 알고 있다. 사용자에게 물을 이유가 없다.
	p := s.cfg.Load()
	p.PhoneMAC = body.MAC
	if p.AdapterMAC == "" {
		p.AdapterMAC = s.sys.BTStatus().AdapterMAC
	}
	if port := s.sys.HFPPort(body.MAC); port > 0 {
		p.HFPPort = port
	}
	rendered, err := s.cfg.Save(p)
	if err != nil {
		// 페어링 자체는 성공했으므로 ok=true 를 유지하되 저장 실패를 알린다.
		writeJSON(w, http.StatusOK, map[string]any{
			"ok": true, "output": out, "save_error": err.Error(),
			"provisioning": p, "missing": p.missing(),
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok": true, "output": out, "rendered": rendered,
		"provisioning": p, "missing": p.missing(),
	})
}

// ── /api/config ──────────────────────────────────────────────────

func (s *Server) handleConfigGet(w http.ResponseWriter, r *http.Request) {
	p := s.cfg.Load()
	writeJSON(w, http.StatusOK, map[string]any{
		"provisioning": p,
		"missing":      p.missing(),
	})
}

func (s *Server) handleConfigSave(w http.ResponseWriter, r *http.Request) {
	var p Provisioning
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		writeErr(w, http.StatusBadRequest, "잘못된 JSON")
		return
	}
	written, err := s.cfg.Save(p)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "rendered": written})
}

// ── /api/service ─────────────────────────────────────────────────

func (s *Server) handleService(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Action string `json:"action"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "action 필요")
		return
	}
	out, ok := s.sys.ServiceAction(body.Action)
	writeJSON(w, statusFor(ok), map[string]any{"ok": ok, "output": out})
}

// ── /api/logs ────────────────────────────────────────────────────

func (s *Server) handleLogs(w http.ResponseWriter, r *http.Request) {
	n := 100
	if q := r.URL.Query().Get("n"); q != "" {
		if v, err := strconv.Atoi(q); err == nil && v > 0 && v <= 1000 {
			n = v
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"logs": s.sys.Logs(n)})
}

// ── 응답 헬퍼 ────────────────────────────────────────────────────

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]any{"error": msg})
}

func statusFor(ok bool) int {
	if ok {
		return http.StatusOK
	}
	return http.StatusBadGateway
}
