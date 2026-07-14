# ClawOps Mobile Gateway

블루투스로 연결한 휴대폰의 **셀룰러 회선**을 ClawOps 에 연결하는 게이트웨이입니다.
리눅스 장비에 설치하면 그 휴대폰 번호로 통화를 주고받을 수 있습니다.

## 요구사항

| 항목 | 지원 |
|---|---|
| OS | glibc 2.35 이상의 데비안 계열 — Ubuntu 22.04 / 24.04, Debian 12 이상 |
| 아키텍처 | amd64 · arm64 |
| 블루투스 | BlueZ 를 지원하는 어댑터 |
| 휴대폰 | 블루투스 핸즈프리(HFP) 를 지원하고 셀룰러 통화가 가능한 기기 |
| 계정 | ClawOps 계정 (등록 토큰 발급에 필요) |

맥과 윈도우에는 설치할 수 없습니다. 리눅스 가상머신에 USB 블루투스 동글을 연결하면 사용할 수 있습니다.

## 설치

게이트웨이 장비에서 실행합니다.

```sh
curl -fsSL https://raw.githubusercontent.com/learners-superpumped/clawops-mobile-gateway/main/scripts/install-release.sh | sudo sh
```

특정 버전을 설치하거나 롤백하려면 버전을 지정합니다.

```sh
curl -fsSL https://raw.githubusercontent.com/learners-superpumped/clawops-mobile-gateway/main/scripts/install-release.sh | sudo VERSION=0.1.2 sh
```

## 설치 후

같은 네트워크의 PC 에서 브라우저로 관리 화면을 엽니다.

```
http://<장비 IP>:8088
```

화면의 5단계를 따라가면 연결이 끝납니다.

**ClawOps 연결** → **블루투스 페어링** → **번호 검증** → **장치 설정** → **서비스 시작**

화면별 자세한 안내는 **[설치 가이드](docs/mobile-gateway-install.md)** 를 참고하세요.

> ⚠️ 관리 화면에는 인증이 없습니다. 신뢰할 수 있는 내부 네트워크에서만 사용하고,
> 인터넷에 직접 공개하지 마세요.

## 문의

[ClawOps 콘솔](https://platform.claw-ops.com) 또는 support@claw-ops.com
