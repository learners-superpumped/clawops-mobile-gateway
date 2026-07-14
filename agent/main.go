// ClawOps Mobile Gateway — control-agent
//
// 박스에 설치되는 root 데몬. 로컬 Web UI + REST API 로 다음을 제어한다:
//   - 상태 조회(asterisk 서비스 · 블루투스 어댑터/페어링 · WireGuard 터널 · chan_mobile)
//   - 블루투스 스캔/페어링
//   - 프로비저닝 값 입력 → 설정 템플릿(.tmpl)을 실제 .conf 로 렌더
//   - 서비스 start/stop/restart · 최근 로그
//
// Web UI 는 embed 로 바이너리에 내장(외부 CDN 없음 = 어플라이언스 원칙).
package main

import (
	"embed"
	"io/fs"
	"log"
	"net/http"
	"os"
	"time"
)

//go:embed web
var webFS embed.FS

func main() {
	addr := envOr("CLAWOPS_AGENT_ADDR", ":8088")

	web, err := fs.Sub(webFS, "web")
	if err != nil {
		log.Fatalf("embed web: %v", err)
	}

	srv := &Server{sys: NewSystem(), cfg: NewConfigManager()}

	mux := http.NewServeMux()
	// API
	mux.HandleFunc("GET /api/status", srv.handleStatus)
	mux.HandleFunc("GET /api/bluetooth/devices", srv.handleBTList)
	mux.HandleFunc("POST /api/bluetooth/scan", srv.handleBTScan)
	mux.HandleFunc("POST /api/bluetooth/pair", srv.handleBTPair)
	mux.HandleFunc("GET /api/config", srv.handleConfigGet)
	mux.HandleFunc("POST /api/config", srv.handleConfigSave)
	mux.HandleFunc("POST /api/enroll", srv.handleEnroll)
	mux.HandleFunc("POST /api/reset", srv.handleReset)
	mux.HandleFunc("GET /api/verification", srv.handleVerification)
	mux.HandleFunc("POST /api/service", srv.handleService)
	mux.HandleFunc("GET /api/logs", srv.handleLogs)
	// Web UI (SPA) — 그 외 전부 index.html/정적자원
	mux.Handle("/", http.FileServer(http.FS(web)))

	httpSrv := &http.Server{
		Addr:              addr,
		Handler:           logRequests(mux),
		ReadHeaderTimeout: 10 * time.Second,
	}

	log.Printf("clawops-agent listening on %s (state=%s)", addr, stateDir)
	if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("serve: %v", err)
	}
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		if r.URL.Path != "/api/status" && r.URL.Path != "/api/verification" { // 폴링은 로그 소음이라 제외
			log.Printf("%s %s %s", r.Method, r.URL.Path, time.Since(start).Round(time.Millisecond))
		}
	})
}
