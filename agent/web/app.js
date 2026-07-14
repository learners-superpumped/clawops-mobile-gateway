const $ = (selector, root = document) => root.querySelector(selector);
const $$ = (selector, root = document) => [...root.querySelectorAll(selector)];

async function api(path, options) {
  const response = await fetch(path, { headers: { "Content-Type": "application/json" }, ...options });
  const body = await response.json().catch(() => ({}));
  return { ok: response.ok, body };
}

function setLevel(selector, level) {
  const element = $(selector);
  if (element) element.className = `dot ${level || ""}`.trim();
}

function setMessage(selector, text, state = "") {
  const element = $(selector);
  element.textContent = text;
  element.className = `msg ${state ? `is-${state}` : ""}`.trim();
}

// bluetoothctl 원문 에러를 사용자가 손 쓸 수 있는 안내로 바꾼다. 원문도 함께 남겨
// (지원 문의 시 단서) 두되, 무슨 뜻인지는 앞에서 설명한다.
function btPairHint(raw) {
  const out = String(raw || "").trim();
  if (/ConnectionAttemptFailed|Connection timed out|Host is down|Page Timeout/i.test(out))
    return "휴대폰에 연결하지 못했습니다. 휴대폰을 게이트웨이의 블루투스 동글 가까이(30cm 이내) 두고 다시 시도하세요.";
  if (/not available/i.test(out))
    return "휴대폰을 찾지 못했습니다. 휴대폰의 블루투스 설정 화면을 열어 검색 허용 상태로 둔 뒤 다시 시도하세요.";
  if (/AuthenticationCanceled|AuthenticationFailed|AuthenticationRejected/i.test(out))
    return "휴대폰에서 페어링이 취소되었습니다. 다시 시도한 뒤 휴대폰에 표시되는 확인을 승인하세요.";
  if (/AlreadyExists/i.test(out))
    return "이미 등록된 장치입니다. 휴대폰의 블루투스 목록에서 이 게이트웨이를 삭제한 뒤 다시 시도하세요.";
  return out || "페어링에 실패했습니다. 다시 시도하세요.";
}

// 국내 전화번호를 사람이 읽는 형태로 — 서버는 정규화된 국내표기(0으로 시작, 구분자 없음)를
// 준다. 규칙은 ClawOps 의 phone-normalize 와 같은 범위(휴대폰/지역/070·050/대표번호)를 다룬다.
function formatKrPhone(raw) {
  const s = String(raw || "").replace(/[^0-9]/g, "");
  if (!s) return "";
  if (/^01[016789]\d{7,8}$/.test(s)) return s.replace(/^(\d{3})(\d{3,4})(\d{4})$/, "$1-$2-$3"); // 010-1234-5678
  if (/^02\d{7,8}$/.test(s)) return s.replace(/^(\d{2})(\d{3,4})(\d{4})$/, "$1-$2-$3"); // 02-123-4567
  if (/^0(50\d|70|3[1-3]|4[1-4]|5[1-5]|6[1-4])\d{7,8}$/.test(s))
    return s.replace(/^(\d{3,4})(\d{3,4})(\d{4})$/, "$1-$2-$3"); // 070-1234-5678 / 0507-1234-5678
  if (/^1[5-9]\d{6}$/.test(s)) return s.replace(/^(\d{4})(\d{4})$/, "$1-$2"); // 1588-1234
  return raw; // 규칙 밖이면 원본 그대로(가공해서 틀리게 보이는 것보다 낫다)
}

// 터널이 "실제로" 붙어 있는지 = 최근 핸드셰이크가 있었는지. wg0 인터페이스 존재만으로는
// 알 수 없다(폐기돼 서버 peer 가 사라져도 인터페이스는 남는다).
const TUNNEL_STALE_SEC = 300;
function tunnelLinkFresh(tunnel) {
  if (!tunnel?.up) return false;
  const hs = Number(tunnel.latest_handshake || 0);
  if (!hs) return false; // 0 = 한 번도 핸드셰이크 없음(예: 번호 검증 전이라 peer 미등록)
  return Date.now() / 1000 - hs < TUNNEL_STALE_SEC;
}

let currentStatus = null;
let journeyInitialized = false;
let activeStep = 0;
let unlockedSteps = [true, false, false, false];
let verified = false; // 번호 검증 통과 여부(state==='verified')
let formDirty = false;
let editingConfig = false;
let reenrolling = false;
let restartRequired = false;
let savedProvisioning = {};

function showStep(index) {
  if (!unlockedSteps[index]) return;
  activeStep = index;
  $$(".step").forEach((step, i) => {
    step.classList.toggle("is-active", i === index);
    step.setAttribute("aria-current", i === index ? "step" : "false");
  });
  $$(".stage").forEach((panel, i) => { panel.hidden = i !== index; });
}

$$('[data-step-target]').forEach((control) => control.addEventListener("click", () => {
  const panel = document.getElementById(control.dataset.stepTarget);
  const index = $$(".stage").indexOf(panel);
  if (index >= 0) showStep(index);
}));

function replaceFormValues(values) {
  Object.entries(values || {}).forEach(([key, value]) => {
    const input = $(`#prov-form [name="${key}"]`);
    if (input) input.value = value || "";
  });
}

function setConfigEditing(editing) {
  const provisioned = Boolean(currentStatus?.config?.provisioned);
  editingConfig = editing || !provisioned;
  const form = $("#prov-form");
  form.classList.toggle("is-readonly", !editingConfig);
  // data-locked(=DID) 는 편집 모드에서도 잠근다 — 검증으로만 확정되는 값이다.
  $$("input", form).forEach((input) => { input.readOnly = !editingConfig || input.hasAttribute("data-locked"); });
  $("#btn-edit-config").hidden = editingConfig || !provisioned;
  $("#btn-cancel-config").hidden = !editingConfig || !provisioned;
  $("#config-mode-copy").textContent = editingConfig ? "변경할 값을 입력한 뒤 저장하세요." : "현재 적용된 설정입니다. 변경하려면 설정 수정을 누르세요.";
  if (!editingConfig) {
    formDirty = false;
    $("#change-banner").hidden = true;
  }
}

function discardConfigChanges() {
  replaceFormValues(savedProvisioning);
  formDirty = false;
  $("#change-banner").hidden = true;
  setMessage("#prov-msg", "");
  setConfigEditing(false);
}

function updateJourney(body) {
  const enrolled = Boolean(body.tunnel?.up || body.config?.provisioning?.tunnel_ip);
  const paired = Boolean(body.bluetooth?.paired?.length);
  const provisioned = Boolean(body.config?.provisioned);
  const running = body.service?.active === "active" && body.chan_mobile?.running;
  // "정상 운영" = 서비스가 떠 있는 것만으로는 부족하다. 번호 검증을 통과해(DID 확정) ClawOps 가
  // 이 게이트웨이를 인정하고, 터널이 실제로 붙어 있어야(핸드셰이크) 통화가 오간다.
  // 재등록하면 서버가 pending_verification 으로 되돌리므로 asterisk 가 옛 설정으로 계속
  // 돌고 있어도 통화는 불가능하다 — 그때 "정상 운영" 이라 하면 거짓말이다.
  const operational = running && verified && tunnelLinkFresh(body.tunnel);
  const completed = [enrolled, paired, verified, operational];
  // 4스텝: 연결 → 페어링 → 번호 검증 → 서비스 시작.
  // 회선 정보(어댑터/폰 MAC·HFP 포트·터널/kamailio IP·DID)는 앞 단계에서 자동으로 채워지므로
  // 별도의 "장치 설정" 단계를 두지 않는다(값 조정은 고급 설정에서).
  unlockedSteps = [true, enrolled, enrolled && paired, enrolled && paired && verified];

  $$(".step").forEach((step, index) => {
    step.classList.toggle("is-complete", completed[index]);
    step.classList.toggle("is-locked", !unlockedSteps[index]);
    step.disabled = !unlockedSteps[index];
    if (!unlockedSteps[index]) step.title = "이전 단계를 먼저 완료하세요";
    else step.removeAttribute("title");
  });

  const next = !enrolled ? 0 : !paired ? 1 : !verified ? 2 : 3;
  const titles = [
    "ClawOps에 연결하세요",
    "휴대폰을 페어링하세요",
    "발신 번호를 검증하세요",
    operational ? "게이트웨이가 동작 중입니다" : !provisioned ? "회선 정보를 확인하세요" : "서비스를 시작하세요",
  ];
  const copies = [
    "등록 토큰을 입력하면 다음 단계가 열립니다.",
    "휴대폰을 연결하면 번호 검증을 진행합니다.",
    "페어링한 휴대폰에서 안내 문자를 보내면 번호가 확정됩니다.",
    operational
      ? "설정을 수정하려면 고급 설정을 여세요."
      : !provisioned
        ? "자동으로 채워지지 않은 값이 있습니다. 고급 설정에서 확인하세요."
        : "준비가 끝났습니다. 서비스를 시작하세요.",
  ];
  $("#next-step-title").textContent = restartRequired ? "서비스를 재시작하세요" : titles[next];
  $("#next-step-copy").textContent = restartRequired ? "저장된 변경사항은 재시작 후 적용됩니다." : copies[next];

  // 검증까지 끝났는데 핸드셰이크가 없으면 진짜 끊김(폐기됐거나 네트워크 장애).
  // ⚠️ verified 일 때만 판정한다 — 검증 전(재등록 직후 포함)에는 서버가 게이트웨이를
  //    pending_verification 으로 두고 WG peer 를 등록하지 않으므로 핸드셰이크가 없는 게
  //    정상이다. 그 구간에 "연결이 끊겼다" 고 하면 멀쩡한 온보딩을 고장으로 오인시킨다.
  const linkDown = running && verified && !tunnelLinkFresh(body.tunnel);
  const badge = $("#lifecycle-badge");
  badge.className = `lifecycle-badge ${restartRequired || linkDown ? "is-attention" : operational ? "is-running" : ""}`.trim();
  badge.textContent = restartRequired
    ? "재시작 필요"
    : linkDown
      ? "연결 끊김"
      : operational
        ? "정상 운영"
        : provisioned && verified
          ? "시작 대기"
          : "설정 중";
  $("#link-down").hidden = !linkDown;
  $("#operation-banner").hidden = !operational || restartRequired;
  $("#restart-required").hidden = !restartRequired;

  $("#enroll-connected").hidden = !enrolled || reenrolling;
  $("#enroll-form").hidden = enrolled && !reenrolling;
  $("#connected-tunnel-ip").textContent = body.config?.provisioning?.tunnel_ip || "연결 확인 중";

  const canStart = enrolled && paired && verified && provisioned;
  $('[data-svc="start"]').disabled = !canStart || body.service?.active === "active";
  $('[data-svc="restart"]').disabled = !canStart || body.service?.active !== "active";
  $('[data-svc="stop"]').disabled = body.service?.active !== "active";

  if (!journeyInitialized) {
    savedProvisioning = { ...(body.config?.provisioning || {}) };
    replaceFormValues(savedProvisioning);
    setConfigEditing(!provisioned);
    showStep(next);
    journeyInitialized = true;
  } else if (!unlockedSteps[activeStep]) showStep(next);
}

async function refreshStatus() {
  const { ok, body } = await api("/api/status");
  if (!ok) {
    $("#health").textContent = "에이전트 오류";
    setLevel("#system-dot", "bad");
    return;
  }
  currentStatus = body;
  const service = body.service?.active;
  const serviceLevel = service === "active" ? "ok" : service === "failed" ? "bad" : "";
  const bluetooth = body.bluetooth || {};
  const bluetoothLevel = bluetooth.adapter_present ? (bluetooth.powered ? "ok" : "warn") : "bad";
  // 헤더의 "시스템" 도 updateJourney 의 operational 과 같은 기준이어야 한다 —
  // 서비스만 떠 있고 검증/터널이 안 된 상태를 "정상" 이라 하면 안 된다.
  const running = body.chan_mobile?.running && service === "active";
  const healthy = running && verified && tunnelLinkFresh(body.tunnel);

  setLevel("#system-dot", healthy ? "ok" : body.config?.provisioned ? "warn" : "");
  $("#health").textContent = healthy ? "정상" : body.config?.provisioned ? "준비됨" : "설정 필요";
  setLevel("#bluetooth-dot", bluetoothLevel);
  $("#bluetooth-health").textContent = bluetooth.adapter_present ? (bluetooth.powered ? "켜짐" : "꺼짐") : "어댑터 없음";
  setLevel("#service-dot", serviceLevel);
  $("#service-health").textContent = service === "active" ? "동작 중" : service === "failed" ? "오류" : "정지됨";

  const enrolled = body.tunnel?.up || body.config?.provisioning?.tunnel_ip;
  // ⚠️ tunnel.up(=wg0 인터페이스 존재)만으로 "연결됨" 을 판정하면 안 된다. ClawOps 에서
  //    게이트웨이를 폐기하면 서버측 peer 가 제거돼 실제 통신은 끊기지만, 박스의 wg0 는
  //    그대로 살아 있어 영원히 "연결됨" 으로 보인다. 실제 도달성의 근거는 핸드셰이크 신선도다.
  //    (WireGuard 는 트래픽이 있으면 ~2분마다 재핸드셰이크 → 5분을 끊김 기준으로 둔다.)
  const linkFresh = tunnelLinkFresh(body.tunnel);
  setLevel("#enroll-dot", linkFresh ? "ok" : enrolled ? "warn" : "");
  $("#enroll-badge").textContent = linkFresh
    ? "연결됨"
    : enrolled
      ? body.tunnel?.up
        ? "연결 끊김"
        : "터널 대기"
      : "미연결";
  setLevel("#summary-bt-dot", bluetoothLevel);
  $("#summary-bt").textContent = bluetooth.paired?.length ? `휴대폰 ${bluetooth.paired.length}대` : bluetooth.powered ? "대기 중" : "사용 불가";
  setLevel("#summary-service-dot", serviceLevel);
  $("#summary-service").textContent = service === "active" ? "동작 중" : service === "failed" ? "오류" : "정지됨";

  if (!formDirty) {
    savedProvisioning = { ...(body.config?.provisioning || {}) };
    replaceFormValues(savedProvisioning);
  }
  updateJourney(body);
}

// 번호 검증 상태 폴링 + 렌더. 상태 갱신(refreshStatus)과 분리된 독립 폴링 — 원격 ClawOps 폴링이
// 느려도(최대 10초) 로컬 상태 UI 가 멈추지 않게 한다. enroll 이후·미검증일 때만 서버를 때리고,
// verified 되면 폴링을 멈춘다(verify-done DOM 은 그대로 유지).
async function refreshVerification() {
  const enrolled = Boolean(currentStatus?.tunnel?.up || currentStatus?.config?.provisioning?.tunnel_ip);
  if (!enrolled) {
    verified = false;
    renderVerification(null);
    return;
  }
  if (verified) return; // 이미 통과 — 서버 폴링·재렌더 중단(DOM 유지).
  const { ok, body } = await api("/api/verification");
  if (!ok) return;
  verified = body.state === "verified";
  renderVerification(body);
  if (currentStatus) updateJourney(currentStatus); // 새 verified 로 여정 재게이트
}

function renderVerification(info) {
  const pending = $("#verify-pending");
  const done = $("#verify-done");
  const blocked = $("#verify-blocked");
  if (!pending || !done || !blocked) return;
  const state = info?.state || "none";

  // 검증 완료
  const isDone = state === "verified";
  done.hidden = !isDone;
  if (isDone) $("#verify-number").textContent = formatKrPhone(info.verified_number) || "—";

  // 차단(만료/quota) — 사유 노출
  const isBlocked = state === "expired" || state === "quota_exceeded" || state === "gone";
  blocked.hidden = !isBlocked;
  if (isBlocked) {
    const map = {
      expired: ["검증 시간이 만료되었습니다", "1단계에서 새 토큰으로 재등록하면 새 검증 코드가 발급됩니다."],
      gone: ["검증 세션을 찾을 수 없습니다", "1단계에서 새 토큰으로 재등록해 다시 시도하세요."],
      quota_exceeded: ["플랜의 회선 한도를 초과했습니다", "ClawOps 콘솔에서 플랜을 상향하거나 회선 부가서비스를 추가한 뒤 다시 문자를 보내세요."],
    };
    $("#verify-blocked-title").textContent = map[state][0];
    $("#verify-blocked-copy").textContent = map[state][1];
  }

  // 대기(안내 + 코드) — pending 이거나 초기 none(enroll 직후 아직 세션 로드 전) 제외
  const showPending = state === "pending";
  pending.hidden = !showPending;
  if (showPending) {
    $("#verify-receive").textContent = formatKrPhone(info.receive_number) || "—";
    $("#verify-code").textContent = info.nonce || "—";
    setLevel("#verify-dot", "warn");
    $("#verify-status-text").textContent = "문자 수신을 기다리는 중… (보낸 뒤 잠시 기다리세요)";
  }
}

$("#btn-reenroll").addEventListener("click", () => {
  reenrolling = true;
  $("#enroll-connected").hidden = true;
  $("#enroll-form").hidden = false;
  setMessage("#enroll-msg", "새 등록 토큰을 사용하면 현재 연결 정보가 교체됩니다.");
  $('#enroll-form [name="enroll_token"]').focus();
});

$("#enroll-form").addEventListener("submit", async (event) => {
  event.preventDefault();
  const form = event.currentTarget;
  const data = new FormData(form);
  const token = String(data.get("enroll_token") || "").trim();
  if (!token) return setMessage("#enroll-msg", "등록 토큰을 입력하세요.", "error");
  const button = form.querySelector('button[type="submit"]');
  button.disabled = true;
  button.textContent = "연결 중…";
  setMessage("#enroll-msg", "보안 연결을 준비하고 있습니다.");
  const { ok, body } = await api("/api/enroll", { method: "POST", body: JSON.stringify({ enroll_token: token }) });
  button.disabled = false;
  button.textContent = "ClawOps에 연결";
  if (ok && body.wg_up) {
    reenrolling = false;
    setMessage("#enroll-msg", "");
    form.querySelector('[name="enroll_token"]').value = "";
    await refreshStatus();
    showStep(1);
  } else setMessage("#enroll-msg", body.error || body.wg_output || "연결하지 못했습니다.", "error");
});

// 연결 해제 = 이 박스의 로컬 상태를 지워 처음 상태로 되돌린다. ClawOps 콘솔에서 폐기한 뒤
// 새로 연결하거나, 박스를 다른 계정으로 옮길 때 쓴다. 서버의 게이트웨이 행은 콘솔에서
// 별도로 폐기해야 한다(박스가 서버 상태를 바꿀 권한은 없다).
$("#btn-reset").addEventListener("click", async (event) => {
  const button = event.currentTarget;
  if (!window.confirm("ClawOps 연결을 해제합니다.\n\n서비스가 중지되고 등록 정보·페어링한 휴대폰·터널 설정이 모두 삭제됩니다. 다시 사용하려면 새 등록 토큰으로 연결해야 합니다.\n\n계속할까요?")) return;
  button.disabled = true;
  button.textContent = "해제 중…";
  const { ok, body } = await api("/api/reset", { method: "POST", body: JSON.stringify({}) });
  button.disabled = false;
  button.textContent = "연결 해제";
  if (!ok) {
    setMessage("#enroll-msg", body?.error || "연결 해제에 실패했습니다.", "error");
    return;
  }
  // 화면 상태도 처음으로 — 폴링이 새 상태를 읽어오지만 여정 초기화는 명시적으로 한다.
  reenrolling = false;
  verified = false;
  journeyInitialized = false;
  await refreshStatus();
  await refreshVerification();
  setMessage("#enroll-msg", "연결이 해제되었습니다. 새 등록 토큰으로 다시 연결하세요.");
});

$("#btn-edit-config").addEventListener("click", () => setConfigEditing(true));
$("#btn-cancel-config").addEventListener("click", discardConfigChanges);
$("#discard-config").addEventListener("click", discardConfigChanges);
$("#prov-form").addEventListener("input", () => {
  if (!editingConfig) return;
  formDirty = true;
  $("#change-banner").hidden = false;
  setMessage("#prov-msg", "");
});

$("#prov-form").addEventListener("submit", async (event) => {
  event.preventDefault();
  const data = new FormData(event.currentTarget);
  const payload = { adapter_mac: data.get("adapter_mac"), phone_mac: data.get("phone_mac"), hfp_port: parseInt(data.get("hfp_port") || "0", 10), tunnel_ip: data.get("tunnel_ip"), kamailio_ip: data.get("kamailio_ip"), did: data.get("did") };
  const wasRunning = currentStatus?.service?.active === "active";
  setMessage("#prov-msg", "설정을 저장하고 있습니다.");
  const { ok, body } = await api("/api/config", { method: "POST", body: JSON.stringify(payload) });
  if (!ok) return setMessage("#prov-msg", body.error || "설정을 저장하지 못했습니다.", "error");
  savedProvisioning = { ...payload };
  formDirty = false;
  restartRequired = wasRunning;
  $("#change-banner").hidden = true;
  setConfigEditing(false);
  setMessage("#prov-msg", "설정을 저장했습니다.", "success");
  await refreshStatus();
  showStep(3);
});

$("#btn-scan").addEventListener("click", async (event) => {
  const button = event.currentTarget;
  button.disabled = true;
  button.textContent = "검색 중…";
  const { body } = await api("/api/bluetooth/scan", { method: "POST" });
  renderBluetooth(body.devices || []);
  button.disabled = false;
  button.textContent = "다시 검색";
});

function renderBluetooth(devices) {
  const list = $("#bt-list");
  if (!devices.length) {
    list.innerHTML = '<div class="empty"><b>검색된 장치가 없습니다</b><span>휴대폰의 블루투스 검색 허용 상태를 확인하고 다시 시도하세요.</span></div>';
    return;
  }
  list.innerHTML = "";
  devices.forEach((device) => {
    const row = document.createElement("div");
    row.className = "list-item";
    const info = document.createElement("div");
    const name = document.createElement("div");
    name.textContent = device.name || "이름 없는 장치";
    const mac = document.createElement("div");
    mac.className = "mac";
    mac.textContent = device.mac;
    info.append(name, mac);
    const button = document.createElement("button");
    button.className = "btn";
    button.textContent = device.paired ? "페어링됨" : "페어링";
    button.disabled = device.paired;
    button.addEventListener("click", async () => {
      button.disabled = true;
      button.textContent = "페어링 중…";
      setMessage("#bt-msg", "");
      const { ok, body } = await api("/api/bluetooth/pair", { method: "POST", body: JSON.stringify({ mac: device.mac }) });
      button.textContent = ok ? "페어링됨" : "다시 시도";
      button.disabled = ok;
      if (!ok) {
        // 실패 원인(ConnectionAttemptFailed=신호 약함/거리, not available=검색 상태 아님 등)을
        // 그대로 보여준다 — 이게 없으면 사용자는 "다시 시도"만 반복하며 이유를 알 수 없다.
        setMessage("#bt-msg", btPairHint(body && (body.output || body.error)), "error");
        return;
      }
      // 폰 MAC·HFP 포트·어댑터 MAC 은 서버(/api/bluetooth/pair)가 이미 확보해 저장했다.
      // 예전처럼 폼에만 채우고 dirty 로 두면 새로고침 한 번에 값이 날아갔다.
      if (body?.missing?.length) {
        setMessage("#bt-msg", "페어링했지만 일부 회선 정보를 자동으로 채우지 못했습니다. 고급 설정에서 확인하세요.", "warn");
      }
      await refreshStatus();
      showStep(2); // 번호 검증으로
    });
    row.append(info, button);
    list.appendChild(row);
  });
}

$$('[data-svc]').forEach((button) => button.addEventListener("click", async () => {
  const action = button.dataset.svc;
  button.disabled = true;
  setMessage("#svc-msg", "요청을 처리하고 있습니다.");
  const { ok, body } = await api("/api/service", { method: "POST", body: JSON.stringify({ action }) });
  if (ok && (action === "restart" || action === "start")) restartRequired = false;
  setMessage("#svc-msg", ok ? "요청을 완료했습니다." : body.output || body.error || "요청하지 못했습니다.", ok ? "success" : "error");
  await refreshStatus();
}));

$("#btn-logs").addEventListener("click", refreshLogs);
async function refreshLogs() {
  const { body } = await api("/api/logs?n=150");
  const lines = (body.logs || "").split("\n").filter(Boolean);
  $("#logs").textContent = lines.length ? lines.reverse().join("\n") : "(로그 없음)";
}

window.addEventListener("beforeunload", (event) => {
  if (!formDirty) return;
  event.preventDefault();
  event.returnValue = "";
});

refreshStatus();
refreshLogs();
setInterval(refreshStatus, 4000);
setInterval(refreshLogs, 5000);
// 검증 폴링 — 상태 폴링과 분리(느린 원격 폴링이 상태 UI 를 막지 않게) + 재진입 방지
// (setInterval 은 이전 폴링을 안 기다려 느린 응답이 겹쳐 쌓임 → 이전 폴링 종료 후 4초 뒤 다음 폴링).
(async function verificationLoop() {
  await refreshVerification();
  setTimeout(verificationLoop, 4000);
})();
