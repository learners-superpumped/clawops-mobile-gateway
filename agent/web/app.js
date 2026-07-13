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

let currentStatus = null;
let journeyInitialized = false;
let activeStep = 0;
let unlockedSteps = [true, false, false, false];
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
  $$("input", form).forEach((input) => { input.readOnly = !editingConfig; });
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
  const completed = [enrolled, paired, provisioned, running];
  unlockedSteps = [true, enrolled, enrolled && paired, enrolled && paired && provisioned];

  $$(".step").forEach((step, index) => {
    step.classList.toggle("is-complete", completed[index]);
    step.classList.toggle("is-locked", !unlockedSteps[index]);
    step.disabled = !unlockedSteps[index];
    if (!unlockedSteps[index]) step.title = "이전 단계를 먼저 완료하세요";
    else step.removeAttribute("title");
  });

  const next = !enrolled ? 0 : !paired ? 1 : !provisioned ? 2 : 3;
  const titles = ["ClawOps에 연결하세요", "휴대폰을 페어링하세요", "장치 설정을 확인하세요", running ? "게이트웨이가 동작 중입니다" : "서비스를 시작하세요"];
  const copies = ["등록 토큰을 입력하면 다음 단계가 열립니다.", "휴대폰을 연결하면 장치 설정을 계속할 수 있습니다.", "회선 정보를 저장하면 서비스를 시작할 수 있습니다.", running ? "설정을 수정하려면 완료된 단계를 선택하세요." : "준비가 끝났습니다. 서비스를 시작하세요."];
  $("#next-step-title").textContent = restartRequired ? "서비스를 재시작하세요" : titles[next];
  $("#next-step-copy").textContent = restartRequired ? "저장된 변경사항은 재시작 후 적용됩니다." : copies[next];

  const badge = $("#lifecycle-badge");
  badge.className = `lifecycle-badge ${restartRequired ? "is-attention" : running ? "is-running" : ""}`.trim();
  badge.textContent = restartRequired ? "재시작 필요" : running ? "정상 운영" : provisioned ? "시작 대기" : "설정 중";
  $("#operation-banner").hidden = !running || restartRequired;
  $("#restart-required").hidden = !restartRequired;

  $("#enroll-connected").hidden = !enrolled || reenrolling;
  $("#enroll-form").hidden = enrolled && !reenrolling;
  $("#connected-tunnel-ip").textContent = body.config?.provisioning?.tunnel_ip || "연결 확인 중";

  const canStart = enrolled && paired && provisioned;
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
  const running = body.chan_mobile?.running && service === "active";

  setLevel("#system-dot", running ? "ok" : body.config?.provisioned ? "warn" : "");
  $("#health").textContent = running ? "정상" : body.config?.provisioned ? "준비됨" : "설정 필요";
  setLevel("#bluetooth-dot", bluetoothLevel);
  $("#bluetooth-health").textContent = bluetooth.adapter_present ? (bluetooth.powered ? "켜짐" : "꺼짐") : "어댑터 없음";
  setLevel("#service-dot", serviceLevel);
  $("#service-health").textContent = service === "active" ? "동작 중" : service === "failed" ? "오류" : "정지됨";

  const enrolled = body.tunnel?.up || body.config?.provisioning?.tunnel_ip;
  setLevel("#enroll-dot", body.tunnel?.up ? "ok" : enrolled ? "warn" : "");
  $("#enroll-badge").textContent = body.tunnel?.up ? "연결됨" : enrolled ? "터널 대기" : "미연결";
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
  const { ok, body } = await api("/api/enroll", { method: "POST", body: JSON.stringify({ enroll_token: token, api_base: String(data.get("api_base") || "").trim(), msisdn: String(data.get("msisdn") || "").trim() }) });
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
      const { ok } = await api("/api/bluetooth/pair", { method: "POST", body: JSON.stringify({ mac: device.mac }) });
      button.textContent = ok ? "페어링됨" : "다시 시도";
      button.disabled = ok;
      if (ok) {
        await refreshStatus();
        setConfigEditing(true);
        const phoneInput = $('#prov-form [name="phone_mac"]');
        phoneInput.value = device.mac;
        formDirty = true;
        $("#change-banner").hidden = false;
        showStep(2);
      }
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
