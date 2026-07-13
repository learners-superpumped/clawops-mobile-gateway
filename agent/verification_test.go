package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// pollVerificationStatus: nonce 를 Authorization: Bearer 로 전송(쿼리스트링 금지) + 200 파싱 + 404 처리.
func TestPollVerificationStatus(t *testing.T) {
	var gotNonce string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/mobile-gateways/verification/status" {
			t.Errorf("경로 = %s", r.URL.Path)
		}
		if r.URL.RawQuery != "" {
			t.Errorf("nonce 가 쿼리스트링에 노출됨: %s", r.URL.RawQuery)
		}
		if b := r.Header.Get("Authorization"); strings.HasPrefix(b, "Bearer ") {
			gotNonce = strings.TrimPrefix(b, "Bearer ")
		}
		if gotNonce == "CODE-GONE0000" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": "verified", "verifiedNumber": "01080541074",
			"receiveNumber": "07052358010", "expiresAt": "2026-07-13T10:00:00.000Z",
		})
	}))
	defer srv.Close()

	vs, code, err := pollVerificationStatus(srv.URL, "CODE-7K9F2MAB")
	if err != nil || code != http.StatusOK {
		t.Fatalf("code=%d err=%v", code, err)
	}
	if vs.Status != "verified" || vs.VerifiedNumber != "01080541074" {
		t.Errorf("파싱 실패: %+v", vs)
	}
	if gotNonce != "CODE-7K9F2MAB" {
		t.Errorf("nonce 전달 안됨: %q", gotNonce)
	}

	_, code404, err := pollVerificationStatus(srv.URL, "CODE-GONE0000")
	if err != nil || code404 != http.StatusNotFound {
		t.Errorf("404 처리: code=%d err=%v", code404, err)
	}
}

// handleVerification: verified 폴링 시 DID 자동 주입 + 캐시.
func TestHandleVerification_VerifiedInjectsDID(t *testing.T) {
	stateDir = t.TempDir()
	sysconfDir = t.TempDir()
	// 렌더할 템플릿 하나(MY_DID 치환 확인).
	_ = os.WriteFile(filepath.Join(sysconfDir, "chan_mobile.conf.tmpl"), []byte("did={{MY_DID}}\n"), 0o644)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": "verified", "verifiedNumber": "01099998888",
			"receiveNumber": "07052358010", "expiresAt": "2026-07-13T10:00:00.000Z",
		})
	}))
	defer srv.Close()

	s := &Server{sys: NewSystem(), cfg: NewConfigManager()}
	if err := s.cfg.SaveVerification(VerificationState{Nonce: "CODE-7K9F2MAB", ReceiveNumber: "07052358010", APIBase: srv.URL}); err != nil {
		t.Fatal(err)
	}

	rr := httptest.NewRecorder()
	s.handleVerification(rr, httptest.NewRequest(http.MethodGet, "/api/verification", nil))
	var body map[string]any
	_ = json.Unmarshal(rr.Body.Bytes(), &body)
	if body["state"] != "verified" || body["verified_number"] != "01099998888" {
		t.Fatalf("응답 = %v", body)
	}
	// DID 가 프로비저닝에 주입됐는지 + 캐시됐는지.
	if did := s.cfg.Load().DID; did != "01099998888" {
		t.Errorf("DID 주입 안됨: %q", did)
	}
	if v := s.cfg.LoadVerification(); v.VerifiedNumber != "01099998888" {
		t.Errorf("검증 캐시 안됨: %+v", v)
	}
	// 템플릿 렌더 확인.
	rendered, _ := os.ReadFile(filepath.Join(sysconfDir, "chan_mobile.conf"))
	if string(rendered) != "did=01099998888\n" {
		t.Errorf("템플릿 렌더 = %q", string(rendered))
	}
}

// handleVerification: enroll 전(세션 없음) → state=none, 서버 폴링 안 함.
func TestHandleVerification_None(t *testing.T) {
	stateDir = t.TempDir()
	s := &Server{sys: NewSystem(), cfg: NewConfigManager()}
	rr := httptest.NewRecorder()
	s.handleVerification(rr, httptest.NewRequest(http.MethodGet, "/api/verification", nil))
	var body map[string]any
	_ = json.Unmarshal(rr.Body.Bytes(), &body)
	if body["state"] != "none" {
		t.Fatalf("state = %v (want none)", body["state"])
	}
}

// enrollResponse 의 verification 필드가 파싱되는지(응답에 있을 때).
func TestCallEnrollAPI_ParsesVerification(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"tunnelIp": "10.20.0.5", "tunnelCidr": "10.20.0.0/16",
			"kamailioIp": "10.20.0.1", "rtpengineIp": "10.20.0.2",
			"rtpPortRange": "10000-20000", "wgGwEndpoint": "203.0.113.10:51820",
			"wgGwPubkey": "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=", "keepalive": 25,
			"verification": map[string]string{"nonce": "CODE-7K9F2MAB", "receiveNumber": "07052358010"},
		})
	}))
	defer srv.Close()

	resp, _, err := callEnrollAPI(srv.URL, "tok", "PUBKEY==", "")
	if err != nil {
		t.Fatal(err)
	}
	if resp.Verification.Nonce != "CODE-7K9F2MAB" || resp.Verification.ReceiveNumber != "07052358010" {
		t.Errorf("verification 파싱 실패: %+v", resp.Verification)
	}
}
