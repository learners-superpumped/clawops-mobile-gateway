#!/bin/sh
# 로컬 .deb 설치 편의 스크립트(리눅스 타깃에서 실행).
#  아키텍처 자동 감지 → dist/<arch>/*.deb 설치 + 의존성 해결.
#
#  최종 배포 형태는 서명된 APT 레포:
#     curl -fsSL https://apt.claw-ops.com/setup.sh | sudo sh
#     sudo apt install clawops-mobile-gateway
#  이 스크립트는 그 전 단계(로컬 빌드 산출물) 설치용.
set -e

ARCH=$(dpkg --print-architecture)
DEB=$(find "dist/${ARCH}" -maxdepth 1 -type f -name "clawops-mobile-gateway_*_${ARCH}.deb" -print 2>/dev/null | sort | head -n 1)

if [ -z "$DEB" ]; then
  echo "!! dist/${ARCH}/ 에 $ARCH .deb 없음. 먼저 'make build-${ARCH}' 실행." >&2
  exit 1
fi

echo ">> installing $DEB"
if command -v apt-get >/dev/null 2>&1; then
  sudo apt-get install -y "./$DEB"
else
  sudo dpkg -i "$DEB"        # set -e: 실패하면 아래 'done' 안 찍고 중단
fi
echo ">> done. 상태: systemctl status clawops-asterisk"
