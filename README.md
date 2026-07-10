# ClawOps Mobile Gateway

블루투스(HFP)로 페어링한 휴대폰의 **셀룰러 회선**을 ClawOps SIP 로 브릿지하는 게이트웨이를
설치형 `.deb` 로 제공한다. 유저가 리눅스 박스(또는 프리번 어플라이언스)에 설치하면
`Asterisk + chan_mobile` 환경이 깔리고, WireGuard 터널로 ClawOps 에 IP-신뢰로 접속한다.

> **왜 소스빌드/vendored 인가**: apt 의 `asterisk-modules` 는 `chan_mobile.so` 를 뺀다(bluez 의존).
> 그래서 Asterisk 22.10.x 를 직접 빌드하고, 배포판 asterisk 와 충돌하지 않도록
> `/opt/clawops-gw` 프리픽스로 vendored 설치한다.

## 상태
🟡 **MVP — 빌드 파이프라인 골격.** 멀티아치(.deb) 빌드까지. control-agent(제어 데몬)·
프로비저닝 API·APT 레포는 후속.

## 요구사항 (지원 매트릭스)
| 항목 | 지원 |
|---|---|
| OS | Ubuntu 22.04 / Debian 12 (glibc ≥ 2.35, pre-t64) · amd64 · arm64 |
| BT | BlueZ 지원 어댑터 (인증 동글 화이트리스트는 후속) |
| ⚠️ 맥/윈도우 | 네이티브 불가. 리눅스 VM + USB 동글 패스스루로만 |

## 빌드
```sh
cd mobile-gateway
make build-arm64     # Apple Silicon 에선 네이티브급으로 빠름
make build-amd64     # mac 에선 QEMU 에뮬(느림)
make list            # dist/<arch>/clawops-mobile-gateway_<ver>_<arch>.deb
```
`docker buildx` 멀티아치를 씀. Docker Desktop 기본 빌더로 동작.

## 설치 (리눅스 타깃)
```sh
sudo sh scripts/install.sh      # dist/<arch>/*.deb 자동 설치
# 설치 후 프로비저닝(BT 페어링·터널·config 치환) 후:
sudo systemctl start clawops-asterisk
```

## 레이아웃
```
mobile-gateway/
├── build/Dockerfile          # asterisk+chan_mobile vendored 빌드 → .deb (buildx 멀티아치)
├── packaging/deb/            # control.in · postinst · prerm
├── files/
│   ├── systemd/              # clawops-asterisk.service (bluetooth·wg0 뒤 기동, root)
│   ├── udev/                 # hci voice=0x0060 영구화 보조
│   ├── bin/clawops-bt-prep.sh# voice 0x0060 강제(매 기동 전)
│   └── config/*.tmpl         # chan_mobile · pjsip(WG) · extensions 템플릿(프로비저닝 시 치환)
├── scripts/install.sh
└── Makefile
```

## 설치 후 프로비저닝(현재 수동 → 후속에서 control-agent 자동화)
1. 휴대폰 BT 페어링 (`bluetoothctl`: scan → pair → trust, connect 는 금지)
2. `/etc/clawops-gw/asterisk/*.tmpl` → 값 치환 후 **`.tmpl` 을 떼고 `.conf` 로 저장** (asterisk 는 `.conf` 만 읽음). 치환값: adapter/phone MAC, HFP port, 터널 IP, kamailio IP, DID
3. WireGuard 터널 기동 (`wg-quick@wg0`)
4. `systemctl start clawops-asterisk`

## 후속 (로드맵)
- **control-agent**: 설치 후 BT 페어링·WG 프로비저닝·config 치환·헬스를 제어하는 root 데몬 + 로컬 Web UI
- **프로비저닝 API + 박스 레지스트리**: 1대→N대. 터널 IP 풀·계정 매핑·크로스테넌트 격리
  (현재 단일 박스 전제의 터널 IP 하드코딩을 레지스트리 기반으로 일반화)
- **APT 레포(GPG 서명)**: `apt install` + CVE OTA 업데이트
- **인증 동글 화이트리스트**, **t64 배포판(Ubuntu 24.04/Debian 13) 빌드 타깃 추가**
