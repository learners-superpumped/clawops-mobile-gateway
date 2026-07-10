package main

import (
	"bufio"
	"context"
	"os/exec"
	"strings"
	"time"
)

// System 은 호스트 명령(systemctl/bluetoothctl/hciconfig/wg/journalctl) 실행을 감싼다.
// 모든 호출은 어댑터/터널 부재 시에도 에러 대신 "비어있음" 상태로 우아하게 처리한다.
type System struct {
	asteriskService string
	adapter         string
	asteriskBin     string
}

func NewSystem() *System {
	return &System{
		asteriskService: envOr("CLAWOPS_SERVICE", "clawops-asterisk"),
		adapter:         envOr("CLAWOPS_HCI", "hci0"),
		asteriskBin:     envOr("CLAWOPS_ASTERISK_BIN", "/opt/clawops-gw/sbin/asterisk"),
	}
}

// run 은 명령을 timeout 과 함께 실행하고 stdout(trim)·성공여부를 돌려준다.
func run(timeout time.Duration, name string, args ...string) (string, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	return strings.TrimSpace(string(out)), err == nil
}

// ── 서비스(asterisk) ─────────────────────────────────────────────

type ServiceStatus struct {
	Name   string `json:"name"`
	Active string `json:"active"` // active|inactive|failed|activating|unknown
}

func (s *System) ServiceStatus() ServiceStatus {
	st, _ := run(5*time.Second, "systemctl", "is-active", s.asteriskService)
	if st == "" {
		st = "unknown"
	}
	return ServiceStatus{Name: s.asteriskService, Active: st}
}

// ServiceAction 은 start|stop|restart 를 수행한다.
//  start/restart 는 --no-block 으로 잡만 큐잉하고 즉시 반환한다 — ExecStartPre(bt-prep)가
//  어댑터를 최대 30초 기다릴 수 있어 블로킹하면 HTTP 요청이 그동안 멈추기 때문.
//  실제 전이(activating→active)는 UI 의 상태 폴링이 보여준다. stop 은 대개 빨라 블록.
func (s *System) ServiceAction(action string) (string, bool) {
	switch action {
	case "start", "restart":
		return run(10*time.Second, "systemctl", action, "--no-block", s.asteriskService)
	case "stop":
		return run(20*time.Second, "systemctl", "stop", s.asteriskService)
	default:
		return "unsupported action", false
	}
}

// ── 블루투스 ─────────────────────────────────────────────────────

type BTStatus struct {
	AdapterPresent bool     `json:"adapter_present"`
	AdapterMAC     string   `json:"adapter_mac,omitempty"`
	Powered        bool     `json:"powered"`
	Paired         []BTDev  `json:"paired,omitempty"`
}

type BTDev struct {
	MAC     string `json:"mac"`
	Name    string `json:"name"`
	Paired  bool   `json:"paired"`
	Trusted bool   `json:"trusted"`
	HFPPort int    `json:"hfp_port,omitempty"`
}

func (s *System) BTStatus() BTStatus {
	var st BTStatus
	// 어댑터 존재/전원 — `hciconfig <hci>` 로 BD 주소 추출
	if out, ok := run(5*time.Second, "hciconfig", s.adapter); ok {
		st.AdapterPresent = true
		st.Powered = strings.Contains(out, "UP RUNNING")
		for _, ln := range strings.Split(out, "\n") {
			if i := strings.Index(ln, "BD Address:"); i >= 0 {
				fields := strings.Fields(ln[i+len("BD Address:"):])
				if len(fields) > 0 {
					st.AdapterMAC = fields[0]
				}
			}
		}
	}
	st.Paired = s.btDevices(true)
	return st
}

// btDevices 는 bluetoothctl 로 장치 목록을 파싱한다. pairedOnly=true 면 페어링된 것만.
func (s *System) btDevices(pairedOnly bool) []BTDev {
	sub := "devices"
	if pairedOnly {
		sub = "devices Paired"
	}
	out, ok := run(8*time.Second, "bluetoothctl", strings.Fields(sub)...)
	if !ok {
		return nil
	}
	var devs []BTDev
	sc := bufio.NewScanner(strings.NewReader(out))
	for sc.Scan() {
		// "Device AA:BB:CC:DD:EE:FF Name Here"
		f := strings.Fields(sc.Text())
		if len(f) < 2 || f[0] != "Device" {
			continue
		}
		devs = append(devs, BTDev{MAC: f[1], Name: strings.Join(f[2:], " ")})
	}
	return devs
}

// ScanDevices 는 짧게 스캔(discovery on→대기→off)한 뒤 발견 목록을 반환한다.
func (s *System) ScanDevices() []BTDev {
	run(2*time.Second, "bluetoothctl", "power", "on")
	run(6*time.Second, "bluetoothctl", "--timeout", "5", "scan", "on")
	return s.btDevices(false)
}

// Pair 는 MAC 을 pair→trust 한다(connect 는 하지 않는다 — chan_mobile 이 잡음).
func (s *System) Pair(mac string) (string, bool) {
	if out, ok := run(20*time.Second, "bluetoothctl", "pair", mac); !ok {
		return out, false
	}
	out, ok := run(8*time.Second, "bluetoothctl", "trust", mac)
	return out, ok
}

// ── WireGuard 터널 ───────────────────────────────────────────────

type TunnelStatus struct {
	Up            bool   `json:"up"`
	Interface     string `json:"interface"`
	LatestHandshk string `json:"latest_handshake,omitempty"`
}

func (s *System) TunnelStatus() TunnelStatus {
	st := TunnelStatus{Interface: "wg0"}
	if out, ok := run(5*time.Second, "wg", "show", "wg0", "latest-handshakes"); ok && out != "" {
		st.Up = true
		fields := strings.Fields(out)
		if len(fields) >= 2 {
			st.LatestHandshk = fields[1]
		}
	}
	return st
}

// ── chan_mobile 모듈 상태 ────────────────────────────────────────

type ChanMobileStatus struct {
	Loaded  bool   `json:"loaded"`
	Running bool   `json:"running"`
	Device  string `json:"device,omitempty"` // Free|Down|... (연결 시)
}

func (s *System) ChanMobileStatus() ChanMobileStatus {
	var st ChanMobileStatus
	out, ok := run(6*time.Second, s.asteriskBin, "-rx", "module show like chan_mobile")
	if !ok {
		return st
	}
	st.Loaded = strings.Contains(out, "chan_mobile.so")
	st.Running = strings.Contains(out, "Running") && !strings.Contains(out, "Not Running")
	// `mobile show devices` CLI 는 모듈이 Running(어댑터 존재) 일 때만 등록된다.
	// Not Running 이면 호출해봐야 "No such command" 라 파싱하지 않는다.
	if st.Running {
		if dev, ok := run(6*time.Second, s.asteriskBin, "-rx", "mobile show devices"); ok &&
			!strings.Contains(dev, "No such command") {
			for _, ln := range strings.Split(dev, "\n") {
				f := strings.Fields(ln)
				if len(f) >= 4 && f[0] != "ID" && strings.Contains(f[1], ":") { // ID Address Connected State ...
					st.Device = f[3]
					break
				}
			}
		}
	}
	return st
}

// Logs 는 서비스의 최근 저널 n줄을 반환한다.
func (s *System) Logs(n int) string {
	out, _ := run(8*time.Second, "journalctl", "-u", s.asteriskService, "--no-pager", "-n", itoa(n))
	return out
}
