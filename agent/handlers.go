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
	Missing      []string     `json:"missing"`      // 아직 안 채운 필드
	Provisioned  bool         `json:"provisioned"`  // 전부 채워짐?
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
	writeJSON(w, statusFor(ok), map[string]any{"ok": ok, "output": out})
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
