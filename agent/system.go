package main

import (
	"bufio"
	"context"
	"os/exec"
	"strconv"
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
//
//	start/restart 는 --no-block 으로 잡만 큐잉하고 즉시 반환한다 — ExecStartPre(bt-prep)가
//	어댑터를 최대 30초 기다릴 수 있어 블로킹하면 HTTP 요청이 그동안 멈추기 때문.
//	실제 전이(activating→active)는 UI 의 상태 폴링이 보여준다. stop 은 대개 빨라 블록.
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
	AdapterPresent bool    `json:"adapter_present"`
	AdapterMAC     string  `json:"adapter_mac,omitempty"`
	Powered        bool    `json:"powered"`
	Paired         []BTDev `json:"paired,omitempty"`
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
	args := []string{"devices"}
	if pairedOnly {
		// ⚠️ `devices Paired` 는 BlueZ 5.65+ 문법 — Ubuntu 22.04(BlueZ 5.64)에선
		//    "Too many arguments" 로 항상 실패해 페어링 목록이 영구히 비고,
		//    UI 가 페어링 완료를 영영 인식하지 못한다(다음 단계가 안 열림).
		//    `paired-devices` 는 5.64 와 그 이후 양쪽에서 동작한다.
		args = []string{"paired-devices"}
	}
	out, ok := run(8*time.Second, "bluetoothctl", args...)
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
		devs = append(devs, BTDev{MAC: f[1], Name: strings.Join(f[2:], " "), Paired: pairedOnly})
	}
	return devs
}

// HFPPort 는 그 폰의 Handsfree(HFP) RFCOMM 채널을 SDP 로 조회한다. 0 이면 조회 실패.
// chan_mobile.conf 의 port= 에 들어가는 값으로, 사용자가 알 수 있는 값이 아니다 —
// 반드시 페어링 직후(폰이 가깝고 깨어 있을 때) 호출해야 성공률이 높다.
func (s *System) HFPPort(mac string) int {
	out, ok := run(25*time.Second, "sdptool", "search", "--bdaddr", mac, "HF")
	if !ok {
		return 0
	}
	for _, ln := range strings.Split(out, "\n") {
		i := strings.Index(ln, "Channel:")
		if i < 0 {
			continue
		}
		if n, err := strconv.Atoi(strings.TrimSpace(ln[i+len("Channel:"):])); err == nil && n > 0 {
			return n
		}
	}
	return 0
}

// isPaired 는 BlueZ 가 실제로 링크 키를 갖고 있는지(Paired: yes) 확인한다.
// bluetoothctl 의 종료 코드는 믿을 수 없으므로(아래 Pair 주석) 이 속성이 유일한 근거다.
func (s *System) isPaired(mac string) bool {
	out, ok := run(8*time.Second, "bluetoothctl", "info", mac)
	if !ok {
		return false
	}
	for _, ln := range strings.Split(out, "\n") {
		if t := strings.TrimSpace(ln); strings.HasPrefix(t, "Paired:") {
			return strings.Contains(t, "yes")
		}
	}
	return false
}

// ScanDevices 는 BR/EDR(클래식) inquiry 로 주변 휴대폰을 찾는다.
//
// ⚠️ `bluetoothctl scan on` 을 쓰지 않는 이유 — 그건 dual(LE+BR/EDR) 스캔이라
//
//	주변 BLE 장치의 랜덤 주소가 대량으로 딸려 들어오고(이름 없는 MAC 십수 개),
//	`bluetoothctl devices` 는 이번 스캔과 무관한 캐시 장치까지 전부 뱉는다.
//	그 목록에서 사용자가 자기 폰을 골라내는 건 사실상 불가능하다.
//	chan_mobile 은 HFP(BR/EDR)만 쓰므로 LE 는 애초에 후보가 아니다.
//	또 기존 5초 타임아웃은 클래식 inquiry(통상 10~30초)엔 너무 짧아 폰이
//	응답하기 전에 끝났다 — 실기에서 폰이 목록에 아예 안 뜨는 원인이었다.
func (s *System) ScanDevices() []BTDev {
	run(2*time.Second, "bluetoothctl", "power", "on")
	// --flush = 이전 inquiry 캐시를 버리고 지금 주변에 있는 것만 본다.
	out, ok := run(40*time.Second, "hcitool", "scan", "--flush")
	if !ok {
		return nil
	}
	var devs []BTDev
	sc := bufio.NewScanner(strings.NewReader(out))
	for sc.Scan() {
		// "\t5C:13:CC:44:82:D6\t이름" — 이름을 못 읽으면 "n/a".
		f := strings.Split(strings.TrimSpace(sc.Text()), "\t")
		if len(f) < 1 || !strings.Contains(f[0], ":") {
			continue // "Scanning ..." 헤더 등
		}
		d := BTDev{MAC: strings.TrimSpace(f[0])}
		if len(f) > 1 {
			if n := strings.TrimSpace(f[1]); n != "" && n != "n/a" {
				d.Name = n
			}
		}
		devs = append(devs, d)
	}
	return devs
}

// Pair 는 MAC 을 pair→trust 한다(connect 는 하지 않는다 — chan_mobile 이 잡음).
// 실패 시 bluetoothctl 의 출력을 그대로 돌려준다 — 원인(ConnectionAttemptFailed 등)이
// UI 에 보여야 사용자가 손을 쓸 수 있다.
func (s *System) Pair(mac string) (string, bool) {
	// 페어링을 받으려면 어댑터가 pairable 이어야 한다. bluetoothctl 은 세션이 끝나면
	// 자기가 켠 값을 되돌리므로 매번 켠다(실기에서 Pairable: no 로 남아 있었다).
	run(5*time.Second, "bluetoothctl", "power", "on")
	run(5*time.Second, "bluetoothctl", "pairable", "on")

	// ⚠️ ScanDevices 는 hcitool(raw HCI inquiry)로 목록을 만든다 — BlueZ 데몬을 거치지
	//    않으므로 bluetoothd 는 그 장치를 모르고, 곧장 pair 하면 "Device ... not available"
	//    로 실패한다. pair 전에 BlueZ discovery 를 돌려 D-Bus 에 등록시킨다.
	run(16*time.Second, "bluetoothctl", "--timeout", "14", "scan", "on")

	out, _ := run(45*time.Second, "bluetoothctl", "pair", mac)

	// ⚠️ 종료 코드를 믿지 않는다 — bluetoothctl 은 페어링에 실패해도 exit 0 을 반환한다.
	//    exit code 만 보면 "Failed to pair: org.bluez.Error.ConnectionAttemptFailed" 를
	//    성공으로 오판해 trust 까지 하고 UI 에 "페어링됨" 을 띄운다(실기에서 Paired:no /
	//    Trusted:yes 라는 모순 상태가 만들어졌다). 실제 Paired 속성만이 근거다.
	if !s.isPaired(mac) {
		if out == "" {
			out = "페어링에 실패했습니다(장치 응답 없음). 휴대폰을 게이트웨이 가까이 두고 다시 시도하세요."
		}
		return out, false
	}

	tout, ok := run(8*time.Second, "bluetoothctl", "trust", mac)
	if !ok {
		return tout, false
	}
	return out, true
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
