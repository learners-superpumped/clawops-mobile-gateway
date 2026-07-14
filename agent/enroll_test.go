package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCallEnrollAPI_Success(t *testing.T) {
	var gotAuth, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		if r.URL.Path != "/v1/mobile-gateways/enroll" {
			t.Errorf("경로 = %s", r.URL.Path)
		}
		// 문서용 더미 값만 사용(RFC5737 TEST-NET + 중립 사설대역). 실제 인프라 값 금지 — public 레포.
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"tunnelIp": "10.20.0.5", "tunnelCidr": "10.20.0.0/16",
			"kamailioIp": "10.20.0.1", "rtpengineIp": "10.20.0.2",
			"rtpPortRange": "10000-20000", "wgGwEndpoint": "203.0.113.10:51820",
			"wgGwPubkey": "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=", "keepalive": 25,
		})
	}))
	defer srv.Close()

	resp, status, err := callEnrollAPI(srv.URL, "tok123", "PUBKEY==")
	if err != nil || status != http.StatusCreated {
		t.Fatalf("status=%d err=%v", status, err)
	}
	if resp.TunnelIP != "10.20.0.5" || resp.WGGwPubkey == "" || resp.RTPEngineIP != "10.20.0.2" {
		t.Errorf("파싱 실패: %+v", resp)
	}
	if gotAuth != "Bearer tok123" {
		t.Errorf("Authorization = %q", gotAuth)
	}
	if !strings.Contains(gotBody, `"wgPubkey":"PUBKEY=="`) {
		t.Errorf("요청 body = %s", gotBody)
	}
}

func TestCallEnrollAPI_TokenRejected(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"토큰 무효"}`))
	}))
	defer srv.Close()

	_, status, err := callEnrollAPI(srv.URL, "bad", "PUBKEY==")
	if err != nil {
		t.Fatalf("err=%v (401 은 err 아니라 status 로 전달돼야)", err)
	}
	if status != http.StatusUnauthorized {
		t.Errorf("status=%d, want 401", status)
	}
}

func TestCallEnrollAPI_MissingTunnelConfig(t *testing.T) {
	// 201 인데 필수 필드 누락 → 에러(불완전 conf 작성 방지).
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"tunnelIp":"10.20.0.5"}`)) // wgGwPubkey/endpoint/kamailio/rtp 누락
	}))
	defer srv.Close()

	_, _, err := callEnrollAPI(srv.URL, "tok", "PUBKEY==")
	if err == nil {
		t.Error("필수 터널 설정 누락인데 에러 없음")
	}
}
