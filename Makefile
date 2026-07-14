# ClawOps Mobile Gateway — 멀티아치 .deb 빌드
# 사용: mobile-gateway/ 에서 `make build-arm64` 등 실행.

PKG_VERSION ?= 0.1.2
BUILD_BASE  ?= ubuntu:22.04
DIST        ?= dist
CTX         := .
DOCKERFILE  := build/Dockerfile

BX = docker buildx build \
       --build-arg BUILD_BASE=$(BUILD_BASE) \
       --build-arg PKG_VERSION=$(PKG_VERSION) \
       --target artifact -f $(DOCKERFILE)

# build-amd64/arm64 는 .PHONY 에 넣지 않는다 — 넣으면 레시피 없는 명시 타깃이 되어
# 아래 build-% 패턴룰을 가린다("Nothing to be done"). 대신 build-% 자체를 .PHONY 처리.
.PHONY: help build clean list build-%

help:
	@echo "make build-arm64   # arm64 .deb → $(DIST)/arm64/   (Apple Silicon 에서 빠름)"
	@echo "make build-amd64   # amd64 .deb → $(DIST)/amd64/   (mac 에선 QEMU 에뮬 = 느림)"
	@echo "make build         # 둘 다"
	@echo "make list          # 산출된 .deb 목록"
	@echo "make clean         # dist/ 삭제"
	@echo ""
	@echo "vars: PKG_VERSION=$(PKG_VERSION)  BUILD_BASE=$(BUILD_BASE)"

build: build-amd64 build-arm64

# arch 를 순수 파라미터로: `make build-amd64` / `make build-arm64` 는 이 패턴 하나로 처리.
build-%:
	$(BX) --platform linux/$* --output type=local,dest=$(DIST)/$* $(CTX)

list:
	@find $(DIST) -name '*.deb' 2>/dev/null | sort || echo "(빌드 산출물 없음)"

clean:
	rm -rf $(DIST)
