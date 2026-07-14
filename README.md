# ClawOps Mobile Gateway

블루투스(HFP)로 페어링한 휴대폰의 **셀룰러 회선**을 ClawOps SIP 로 브릿지하는 게이트웨이를
설치형 `.deb` 로 제공한다. 유저가 리눅스 박스(또는 프리번 어플라이언스)에 설치하면
`Asterisk + chan_mobile` 환경이 깔리고, WireGuard 터널로 ClawOps 에 IP-신뢰로 접속한다.
설치 후 **로컬 Web UI(`:8088`)** 에서 블루투스 페어링·프로비저닝·서비스 제어를 GUI 로 한다.

> **왜 소스빌드/vendored 인가**: apt 의 `asterisk-modules` 는 `chan_mobile.so` 를 뺀다(bluez 의존).
> 그래서 Asterisk 22.10.x 를 직접 빌드하고, 배포판 asterisk 와 충돌하지 않도록
> `/opt/clawops-gw` 프리픽스로 vendored 설치한다.

## 상태
🟢 **`.deb`(멀티아치) + control-agent(로컬 Web UI/프로비저닝 API) 구현.** OrbStack Ubuntu 22.04
실기 설치→기동 검증 완료. 프로비저닝 API + 박스 레지스트리(1대→N대)·APT 레포는 후속.

## 요구사항 (지원 매트릭스)
| 항목 | 지원 |
|---|---|
| OS | **glibc ≥ 2.35 인 데비안 계열** — Ubuntu 22.04 / 24.04, Debian 12+ (파생 배포판 포함) |
| 아키텍처 | amd64 · arm64 |
| BT | BlueZ 지원 어댑터 (인증 동글 화이트리스트는 후속) |
| ⚠️ 맥/윈도우 | 네이티브 불가. 리눅스 VM + USB 동글 패스스루로만 |

> **배포판별 빌드는 필요 없다.** 실제 하한은 배포판 버전이 아니라 **glibc**(빌드 베이스
> `ubuntu:22.04` = 2.35)이고, 이건 하한이라 그 이상에선 그대로 돈다. 22.04(amd64)·24.04(arm64)
> 실기 설치로 검증했다. 늘려야 할 축은 아키텍처(amd64/arm64)뿐.
>
> t64(64-bit `time_t`) 전환은 문제가 되지 않는다 — `libbluetooth3` 는 애초에 전환 대상이
> 아니라 24.04 에도 같은 이름으로 존재하고, amd64/arm64 는 원래 `time_t` 가 64비트다.

## 빌드
```sh
make build-arm64     # Apple Silicon 에선 네이티브급으로 빠름
make build-amd64     # mac 에선 QEMU 에뮬(느림)
make list            # dist/<arch>/clawops-mobile-gateway_<ver>_<arch>.deb
```
`docker buildx` 멀티아치. asterisk 는 소스빌드, Go control-agent 는 `$BUILDPLATFORM` 에서
크로스컴파일(QEMU 회피). Docker Desktop 기본 빌더로 동작.

## 설치 & 프로비저닝 (리눅스 타깃)
```sh
sudo sh scripts/install.sh      # dist/<arch>/*.deb 자동 설치
```
설치되면 **control-agent 가 자동 기동**한다. 브라우저로:
```
http://<이 박스 IP>:8088
```
→ ① 블루투스 페어링 ② 값 입력(어댑터/폰 MAC·HFP포트·터널IP·kamailio IP·DID) → `.conf` 자동 렌더
③ 서비스 시작 · 로그 확인. (asterisk 는 프로비저닝 완료 후에만 기동)

> ⚠️ **보안(MVP)**: control-agent 는 인증 없이 `:8088` 전 인터페이스에 바인드 —
> 신뢰된 LAN 전제. 후속에서 토큰 인증/바인드 제한 예정.

## 레이아웃
```
clawops-mobile-gateway/            # 이 레포 루트
├── agent/                    # control-agent (Go) — 로컬 Web UI + 프로비저닝 API
│   ├── main.go · system.go · config.go · handlers.go
│   └── web/                  # embed 되는 self-contained SPA (index.html/style.css/app.js)
├── build/Dockerfile          # asterisk 소스빌드 + agent 크로스컴파일 → .deb (buildx 멀티아치)
├── packaging/deb/            # control.in · postinst · prerm
├── files/
│   ├── systemd/              # clawops-asterisk.service · clawops-agent.service
│   ├── udev/                 # hci voice=0x0060 영구화 보조
│   ├── bin/clawops-bt-prep.sh# voice 0x0060 강제(매 기동 전)
│   └── config/*.tmpl         # chan_mobile · pjsip(WG) · extensions 템플릿(agent 가 .conf 로 렌더)
├── scripts/install.sh
└── Makefile
```

## 수동 프로비저닝 (Web UI 대신)
1. 휴대폰 BT 페어링 (`bluetoothctl`: scan → pair → trust, connect 는 금지)
2. `/etc/clawops-gw/asterisk/*.tmpl` → 값 치환 후 **`.tmpl` 을 떼고 `.conf` 로 저장** (asterisk 는 `.conf` 만 읽음)
3. WireGuard 터널 기동 (`wg-quick@wg0`)
4. `systemctl start clawops-asterisk`

## 후속 (로드맵)
- **control-agent 심화**: WireGuard 자동 프로비저닝(콜홈)·인증·`.conf` 상태 검증
- **프로비저닝 API + 박스 레지스트리**: 1대→N대. 터널 IP 풀·계정 매핑·크로스테넌트 격리
  (현재 단일 박스 전제의 터널 IP 하드코딩을 레지스트리 기반으로 일반화)
- **APT 레포(GPG 서명)**: `apt install` + CVE OTA 업데이트
- **인증 동글 화이트리스트**
