#!/bin/sh
# hci 어댑터가 올라올 때까지 대기 후 HFP SCO 에 필요한 voice=0x0060 강제.
#  chan_mobile 은 voice!=0x0060 이면 어댑터를 거부(declined)한다.
#  (재부팅/어댑터 reset 시 초기화되므로 매 기동 전 clawops-asterisk.service 의
#   ExecStartPre 로 호출된다. udev 규칙은 보조 경로.)
set -e
ADAPTER="${CLAWOPS_HCI:-hci0}"

i=0
while [ "$i" -lt 30 ]; do
  if hciconfig "$ADAPTER" >/dev/null 2>&1; then break; fi
  i=$((i + 1))
  sleep 1
done

hciconfig "$ADAPTER" up 2>/dev/null || true
hciconfig "$ADAPTER" voice 0x0060
echo "[bt-prep] $ADAPTER voice=0x0060 set"
