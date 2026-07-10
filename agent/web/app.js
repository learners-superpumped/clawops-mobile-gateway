// ClawOps Mobile Gateway — control-agent UI 로직 (의존성 없음)
const $ = (s, r = document) => r.querySelector(s);
const $$ = (s, r = document) => [...r.querySelectorAll(s)];

async function api(path, opts) {
  const res = await fetch(path, { headers: { "Content-Type": "application/json" }, ...opts });
  const body = await res.json().catch(() => ({}));
  return { ok: res.ok, body };
}

function setDot(cardSel, level) {
  $(cardSel + " [data-dot]").className = "dot " + level;
}
function setCard(cardSel, value, sub) {
  $(cardSel + " [data-v]").textContent = value;
  if (sub != null) $(cardSel + " [data-s]").textContent = sub;
}

// ── 상태 폴링 ─────────────────────────────────────────────
async function refreshStatus() {
  const { ok, body } = await api("/api/status");
  const health = $("#health");
  if (!ok) { health.className = "pill pill-bad"; health.textContent = "에이전트 오류"; return; }

  // 서비스
  const svc = body.service.active;
  const svcLevel = svc === "active" ? "ok" : svc === "failed" ? "bad" : "warn";
  setDot("#c-service", svcLevel); setCard("#c-service", svc, body.service.name);

  // 블루투스
  const bt = body.bluetooth;
  setDot("#c-bt", bt.adapter_present ? (bt.powered ? "ok" : "warn") : "bad");
  setCard("#c-bt", bt.adapter_present ? (bt.powered ? "켜짐" : "꺼짐") : "없음",
    bt.paired && bt.paired.length ? `페어링 ${bt.paired.length}` : "미페어링");

  // 터널
  setDot("#c-tunnel", body.tunnel.up ? "ok" : "bad");
  setCard("#c-tunnel", body.tunnel.up ? "연결" : "끊김", body.tunnel.interface);

  // chan_mobile
  const cm = body.chan_mobile;
  setDot("#c-mobile", cm.running ? "ok" : cm.loaded ? "warn" : "bad");
  setCard("#c-mobile", cm.running ? "동작" : cm.loaded ? "로드됨" : "미로드", cm.device || "장치없음");

  // 종합 헬스
  const prov = body.config.provisioned;
  if (svc === "active" && cm.running) { health.className = "pill pill-ok"; health.textContent = "정상 동작"; }
  else if (!prov) { health.className = "pill pill-warn"; health.textContent = "프로비저닝 필요"; }
  else { health.className = "pill pill-warn"; health.textContent = "대기"; }

  // 프로비저닝 배지 + 폼 프리필(사용자가 편집 중이 아닐 때만)
  const badge = $("#prov-badge");
  if (prov) { badge.className = "pill pill-ok"; badge.textContent = "완료"; }
  else { badge.className = "pill pill-warn"; badge.textContent = `미완: ${body.config.missing.join(", ")}`; }
  if (!formDirty) fillForm(body.config.provisioning);
}

// ── 프로비저닝 폼 ─────────────────────────────────────────
let formDirty = false;
$("#prov-form").addEventListener("input", () => { formDirty = true; });

function fillForm(p) {
  for (const [k, v] of Object.entries(p)) {
    const el = $(`#prov-form [name="${k}"]`);
    if (el && !el.value) el.value = v || "";
  }
}

$("#prov-form").addEventListener("submit", async (e) => {
  e.preventDefault();
  const fd = new FormData(e.target);
  const payload = {
    adapter_mac: fd.get("adapter_mac"), phone_mac: fd.get("phone_mac"),
    hfp_port: parseInt(fd.get("hfp_port") || "0", 10),
    tunnel_ip: fd.get("tunnel_ip"), kamailio_ip: fd.get("kamailio_ip"), did: fd.get("did"),
  };
  const msg = $("#prov-msg"); msg.textContent = "저장 중…";
  const { ok, body } = await api("/api/config", { method: "POST", body: JSON.stringify(payload) });
  if (ok) { msg.textContent = `✓ 렌더됨: ${(body.rendered || []).join(", ") || "없음"}`; formDirty = false; }
  else { msg.textContent = "✗ " + (body.error || "실패"); }
  refreshStatus();
});

// ── 블루투스 ──────────────────────────────────────────────
$("#btn-scan").addEventListener("click", async (e) => {
  const btn = e.target; btn.disabled = true; btn.textContent = "스캔 중…";
  const { body } = await api("/api/bluetooth/scan", { method: "POST" });
  renderBT(body.devices || []);
  btn.disabled = false; btn.textContent = "스캔";
});

function renderBT(devs) {
  const list = $("#bt-list");
  if (!devs.length) { list.innerHTML = '<div class="empty">장치 없음. 휴대폰을 페어링 모드로 두고 다시 스캔하세요.</div>'; return; }
  list.innerHTML = "";
  for (const d of devs) {
    const row = document.createElement("div");
    row.className = "list-item";
    row.innerHTML = `<div><div>${escapeHtml(d.name || "(이름없음)")}</div><div class="mac">${escapeHtml(d.mac)}</div></div>`;
    const b = document.createElement("button");
    b.className = "btn"; b.textContent = d.paired ? "페어링됨" : "페어링"; b.disabled = d.paired;
    b.onclick = async () => {
      b.disabled = true; b.textContent = "페어링 중…";
      const { ok } = await api("/api/bluetooth/pair", { method: "POST", body: JSON.stringify({ mac: d.mac }) });
      b.textContent = ok ? "페어링됨" : "실패"; b.disabled = ok;
      if (ok) fillMacFromPair(d.mac);
      refreshStatus();
    };
    row.appendChild(b); list.appendChild(row);
  }
}
function fillMacFromPair(mac) {
  const el = $('#prov-form [name="phone_mac"]');
  if (el && !el.value) { el.value = mac; formDirty = true; }
}

// ── 서비스 제어 ───────────────────────────────────────────
$$("[data-svc]").forEach((btn) =>
  btn.addEventListener("click", async () => {
    const action = btn.dataset.svc;
    const msg = $("#svc-msg"); msg.textContent = `${action}…`;
    const { ok, body } = await api("/api/service", { method: "POST", body: JSON.stringify({ action }) });
    // start/restart 는 --no-block(큐잉) — 실제 전이는 상태 카드가 보여준다.
    msg.textContent = ok ? `✓ ${action} 요청됨` : `✗ ${body.output || body.error || "실패"}`;
    refreshStatus();
  })
);

// ── 로그 ──────────────────────────────────────────────────
$("#btn-logs").addEventListener("click", refreshLogs);
async function refreshLogs() {
  const { body } = await api("/api/logs?n=120");
  $("#logs").textContent = body.logs || "(로그 없음)";
}

function escapeHtml(s) { return (s || "").replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c])); }

// ── 부트 ──────────────────────────────────────────────────
refreshStatus();
refreshLogs();
setInterval(refreshStatus, 4000);
