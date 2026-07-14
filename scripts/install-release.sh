#!/bin/sh
# ClawOps Mobile Gateway — GitHub Release 원클릭 설치기
set -eu

REPO="learners-superpumped/clawops-mobile-gateway"
API="https://api.github.com/repos/$REPO"

die() {
  echo "오류: $*" >&2
  exit 1
}

[ "$(id -u)" -eq 0 ] || die "root 권한이 필요합니다. curl ... | sudo sh 로 실행하세요."
command -v curl >/dev/null 2>&1 || die "curl이 필요합니다."
command -v dpkg >/dev/null 2>&1 || die "Ubuntu 또는 Debian에서만 설치할 수 있습니다."
command -v sha256sum >/dev/null 2>&1 || die "sha256sum이 필요합니다."

# 데비안 계열이면 통과 — 배포판 이름/버전이 아니라 아래의 glibc·아키텍처가 실제 제약이다.
# ID_LIKE 까지 보므로 Mint/Pop!_OS/Raspbian 등 파생 배포판도 커버한다.
if [ -r /etc/os-release ]; then
  # ⚠️ 서브셸에서 읽는다 — /etc/os-release 는 VERSION("22.04.5 LTS (Jammy Jellyfish)") 을
  #    정의하므로 현재 셸에서 source 하면 설치할 패키지 버전인 VERSION 을 덮어써
  #    TAG 가 "v22.04.5 LTS (Jammy Jellyfish)" 가 되고 다운로드 URL 이 깨진다.
  # shellcheck disable=SC1091
  OS_INFO=$(. /etc/os-release 2>/dev/null; printf '%s %s' "${ID:-}" "${ID_LIKE:-}")
  case " $OS_INFO " in
    *" ubuntu "*|*" debian "*|*ubuntu*|*debian*) ;;
    *) die "데비안 계열(Ubuntu/Debian)에서만 설치할 수 있습니다: ${OS_INFO% *}" ;;
  esac
fi

ARCH=$(dpkg --print-architecture)
case "$ARCH" in
  amd64|arm64) ;;
  *) die "지원하지 않는 아키텍처입니다: $ARCH (amd64 또는 arm64 필요)" ;;
esac

# 실제 하한 = glibc. 패키지는 ubuntu:22.04(glibc 2.35)에서 빌드하므로 그 이상이면 배포판/버전을
# 가리지 않고 동작한다(22.04·24.04 실기 검증). 이 검사가 없으면 glibc 가 낮은 환경(20.04=2.31)에서
# 설치는 성공하고 asterisk 만 나중에 조용히 죽는다 — 여기서 명확히 막는다.
MIN_GLIBC=2.35
GLIBC=$(ldd --version 2>/dev/null | head -n 1 | grep -oE '[0-9]+\.[0-9]+$' || true)
if [ -n "$GLIBC" ]; then
  OLDEST=$(printf '%s\n%s\n' "$MIN_GLIBC" "$GLIBC" | sort -V | head -n 1)
  [ "$OLDEST" = "$MIN_GLIBC" ] ||
    die "glibc $MIN_GLIBC 이상이 필요합니다(현재 $GLIBC). Ubuntu 22.04+ 또는 Debian 12+ 를 사용하세요."
fi

if [ -n "${VERSION:-}" ]; then
  VERSION=${VERSION#v}
  TAG="v$VERSION"
else
  TAG=$(curl -fsSL -H "Accept: application/vnd.github+json" "$API/releases/latest" |
    sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' |
    head -n 1)
  [ -n "$TAG" ] || die "최신 GitHub Release를 찾지 못했습니다."
  VERSION=${TAG#v}
fi

FILE="clawops-mobile-gateway_${VERSION}_${ARCH}.deb"
BASE="https://github.com/$REPO/releases/download/$TAG"
TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT INT TERM

echo "ClawOps Mobile Gateway $VERSION ($ARCH) 다운로드 중..."
curl -fL --retry 3 -o "$TMP/$FILE" "$BASE/$FILE"
curl -fL --retry 3 -o "$TMP/SHA256SUMS" "$BASE/SHA256SUMS"

EXPECTED=$(sed -n "s/^\([0-9a-fA-F][0-9a-fA-F]*\)[[:space:]][[:space:]]*$FILE$/\1/p" "$TMP/SHA256SUMS" | head -n 1)
[ -n "$EXPECTED" ] || die "SHA256SUMS에 $FILE 항목이 없습니다."
ACTUAL=$(sha256sum "$TMP/$FILE" | awk '{print $1}')
[ "$EXPECTED" = "$ACTUAL" ] || die "패키지 체크섬이 일치하지 않습니다."

echo "패키지 검증 완료. 설치 중..."
apt-get install -y "$TMP/$FILE"
systemctl enable --now clawops-agent

IP=$(hostname -I 2>/dev/null | awk '{print $1}')
[ -n "$IP" ] || IP="<게이트웨이-IP>"

echo
echo "ClawOps Mobile Gateway $VERSION 설치 완료"
echo "관리 화면: http://$IP:8088"
echo
echo "다음 단계:"
echo "1. ClawOps 콘솔 → 실험실 → 모바일 게이트웨이"
echo "2. 게이트웨이 등록 → 토큰 복사"
echo "3. 관리 화면에 토큰 붙여넣기"
