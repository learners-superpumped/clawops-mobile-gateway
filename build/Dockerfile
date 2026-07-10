# syntax=docker/dockerfile:1
# ─────────────────────────────────────────────────────────────────────────────
# ClawOps Mobile Gateway — Asterisk 22.10.x + chan_mobile 를 /opt/clawops-gw 로
# vendored 빌드하고 아키텍처별 .deb 로 패키징한다. buildx 멀티아치(amd64/arm64).
#
#  · ClawOps 프로덕션 Asterisk 빌드 레시피에서 BT 게이트웨이에 필요한 것만 트리밍:
#      + libbluetooth-dev / chan_mobile              (핵심)
#      - odbc/opus/speex/vorbis/pq/spandsp/jack/…    (게이트웨이 불필요 → 용량·공격면 축소)
#  · vendored prefix(/opt/clawops-gw)로 배포판 asterisk 와 충돌 회피.
#  · BUILD_NATIVE off: 빌드머신 CPU 세대에 맞춰 컴파일 → 유저 CPU 에서 SIGILL 방지(재배포 필수).
#  · ASTERISK_VERSION·tarball URL 은 상단 ARG 에 고정(bump 시 한 곳만 수정).
#    pjproject(bundled) 는 -j 병렬 시 libpjnath.a 레이스가 있어 third-party 만 -j1(아래 참조).
#
# 산출 추출: `--target artifact` 로 빌드하면 마지막 stage 에 .deb 만 남아
#            `docker buildx build --output type=local,dest=dist/<arch>` 로 뽑힌다.
# ─────────────────────────────────────────────────────────────────────────────

# glibc 하한을 낮게 잡아 상위 배포판까지 커버(22.04=glibc2.35 → 22.04/24.04·Debian12 실행).
ARG BUILD_BASE=ubuntu:22.04
ARG ASTERISK_VERSION=22.10.1
ARG PREFIX=/opt/clawops-gw
ARG SYSCONFDIR=/etc/clawops-gw
ARG LOCALSTATEDIR=/var/lib/clawops-gw

# ── Stage 1: builder ─────────────────────────────────────────────────────────
FROM ${BUILD_BASE} AS builder
ARG ASTERISK_VERSION
ARG PREFIX
ARG SYSCONFDIR
ARG LOCALSTATEDIR
ENV DEBIAN_FRONTEND=noninteractive

# BT 게이트웨이 최소 빌드 의존성.
#  libbluetooth-dev → chan_mobile 자동 enable.
#  ⚠️ binutils-dev(libbfd) 는 일부러 제외 — BETTER_BACKTRACES 를 켜지 않는다(아래 참조).
#  apt 캐시 마운트로 재빌드·멀티아치 시 메타데이터 재다운로드 회피(빌더 stage 폐기되므로 lists 정리 불필요).
RUN --mount=type=cache,target=/var/cache/apt,sharing=locked \
    --mount=type=cache,target=/var/lib/apt,sharing=locked \
    rm -f /etc/apt/apt.conf.d/docker-clean \
    && apt-get update && apt-get install -y --no-install-recommends \
      build-essential wget ca-certificates pkg-config patch \
      libxml2-dev libncurses-dev libsqlite3-dev libssl-dev uuid-dev \
      libjansson-dev libedit-dev libcurl4-openssl-dev libsrtp2-dev \
      libbluetooth-dev

WORKDIR /usr/src
# 소스 tarball 캐시 마운트 — 재빌드 시 재다운로드 회피(아키텍처 무관 소스라 양 아키텍처가 공유).
RUN --mount=type=cache,target=/var/cache/clawops-dl \
      F="/var/cache/clawops-dl/asterisk-${ASTERISK_VERSION}.tar.gz"; \
      [ -f "$F" ] || wget -qO "$F" \
        "https://downloads.asterisk.org/pub/telephony/asterisk/asterisk-${ASTERISK_VERSION}.tar.gz"; \
      tar xzf "$F"
WORKDIR /usr/src/asterisk-${ASTERISK_VERSION}

# vendored prefix + bundled pjproject(2.17).
RUN ./configure \
      --prefix=${PREFIX} \
      --sysconfdir=${SYSCONFDIR} \
      --localstatedir=${LOCALSTATEDIR} \
      --with-pjproject-bundled

# menuselect:
#  - BUILD_NATIVE off(SIGILL 방지) · chan_mobile enable(addon).
#  - ⚠️ BETTER_BACKTRACES 는 켜지 않는다: libbfd(binutils) 에 링크되는데 libbfd 는
#    SONAME 이 버전에 고정(libbfd-2.38-system.so 등)이라 빌드 배포판에 종속 →
#    재배포 .deb 가 다른 배포판(Debian12 등)에서 로드 실패. 이식성 위해 제거.
#  - chan_mobile 은 libbluetooth-dev 존재 시 자동 선택될 수 있어 enable 은 best-effort.
#    실제 존재 여부는 아래 make install 후 chan_mobile.so 로 강검증.
#  chan_mobile enable 은 best-effort({ … || true }) — 진짜 검증은 아래 chan_mobile.so 강검증.
RUN make menuselect.makeopts \
    && menuselect/menuselect --disable BUILD_NATIVE menuselect.makeopts \
    && { menuselect/menuselect --enable chan_mobile menuselect.makeopts || true; } \
    && menuselect/menuselect --check-deps menuselect.makeopts

# pjproject(bundled) 는 -j 병렬 시 libpjnath.a 레이스 → third-party 만 -j1, 본체는 -j.
RUN make -C third-party -j1 \
    && make -j"$(nproc)" \
    && make install DESTDIR=/stage \
    && make samples DESTDIR=/stage

# chan_mobile.so 강검증 — 없으면 여기서 빌드 실패시켜 조기 발견.
RUN test -f "/stage${PREFIX}/lib/asterisk/modules/chan_mobile.so" \
    || (echo "!! chan_mobile.so 누락 — libbluetooth-dev/menuselect 확인" && exit 1)

# ── Stage 2: packager ────────────────────────────────────────────────────────
FROM ${BUILD_BASE} AS packager
ARG PREFIX
ARG SYSCONFDIR
ARG PKG_VERSION=0.1.0
ARG TARGETARCH
ENV DEBIAN_FRONTEND=noninteractive
RUN apt-get update && apt-get install -y --no-install-recommends dpkg-dev \
    && rm -rf /var/lib/apt/lists/*

# 1) vendored asterisk 산출물을 패키지 루트로.
COPY --from=builder /stage /pkgroot
# 2) 패키징 자산(systemd/udev/bin/config 템플릿) + DEBIAN 메타.
COPY packaging/deb/ /debian-meta/
COPY files/ /files/

RUN set -eux; \
    install -d /pkgroot/lib/systemd/system /pkgroot/lib/udev/rules.d \
              /pkgroot/usr/local/bin "/pkgroot${SYSCONFDIR}/asterisk" /pkgroot/DEBIAN /out; \
    cp /files/systemd/*.service /pkgroot/lib/systemd/system/; \
    cp /files/udev/*.rules      /pkgroot/lib/udev/rules.d/; \
    cp /files/bin/*.sh          /pkgroot/usr/local/bin/; chmod 0755 /pkgroot/usr/local/bin/*.sh; \
    # 템플릿은 astetcdir(sysconfdir/asterisk) 에 둔다 — asterisk 가 실제로 읽는 위치.
    cp /files/config/*.tmpl     "/pkgroot${SYSCONFDIR}/asterisk/"; \
    sed -e "s/@VERSION@/${PKG_VERSION}/" -e "s/@ARCH@/${TARGETARCH}/" \
        /debian-meta/control.in > /pkgroot/DEBIAN/control; \
    cp /debian-meta/postinst /debian-meta/prerm /pkgroot/DEBIAN/; \
    chmod 0755 /pkgroot/DEBIAN/postinst /pkgroot/DEBIAN/prerm; \
    dpkg-deb --build --root-owner-group /pkgroot \
      "/out/clawops-mobile-gateway_${PKG_VERSION}_${TARGETARCH}.deb"; \
    ls -la /out

# ── Stage 3: artifact(추출 전용) ─────────────────────────────────────────────
FROM scratch AS artifact
COPY --from=packager /out/ /
