"use strict";

// ---- state ----------------------------------------------------------------
const counts = { 200: 0, 429: 0, other: 0 };
const upstreams = {}; // upstream URL -> count

const $ = (id) => document.getElementById(id);

// ---- sending requests ------------------------------------------------------
async function send() {
  const method = $("method").value;
  const path = $("path").value.trim() || "/";
  const headers = parseHeaders($("headers").value);
  const body = $("body").value;

  const started = performance.now();
  let resp, text;
  try {
    // Do not follow redirects; report redirects as their own status so the
    // user sees exactly what the gateway returned.
    resp = await fetch(path, { method, headers, body, redirect: "manual" });
    text = (await resp.text()).slice(0, 2000);
  } catch (err) {
    $("response").textContent = "Request failed: " + err.message;
    return;
  }
  const ms = Math.round(performance.now() - started);

  const status = resp.status;
  counts[status === 200 ? "200" : status === 429 ? "429" : "other"]++;

  const upstream = resp.headers.get("x-upstream") || "(none)";
  if (upstream) upstreams[upstream] = (upstreams[upstream] || 0) + 1;

  $("c200").textContent = counts["200"];
  $("c429").textContent = counts["429"];
  $("cOther").textContent = counts["other"];

  $("meta").textContent =
    `${method} ${path} → ${status} in ${ms}ms · upstream: ${upstream}`;
  $("response").textContent = text || "(empty body)";

  renderChart();
}

function parseHeaders(text) {
  const out = {};
  for (const line of text.split("\n")) {
    const idx = line.indexOf(":");
    if (idx > 0) out[line.slice(0, idx).trim()] = line.slice(idx + 1).trim();
  }
  return out;
}

// ---- tiny dependency-free SVG bar chart ------------------------------------
function renderChart() {
  const entries = Object.entries(upstreams);
  const el = $("chart");
  el.innerHTML = "";
  if (entries.length === 0) {
    el.textContent = "Send requests to see the split.";
    return;
  }
  const max = Math.max(...entries.map(([, v]) => v));
  const W = 320, H = 120, pad = 8;
  const svg = document.createElementNS("http://www.w3.org/2000/svg", "svg");
  svg.setAttribute("viewBox", `0 0 ${W} ${H}`);
  const bw = (W - pad * 2) / entries.length;
  entries.forEach(([name, v], i) => {
    const bh = (v / max) * (H - 40);
    const x = pad + i * bw + bw * 0.15;
    const w = bw * 0.7;
    const rect = document.createElementNS("http://www.w3.org/2000/svg", "rect");
    rect.setAttribute("x", x);
    rect.setAttribute("y", H - pad - bh);
    rect.setAttribute("width", w);
    rect.setAttribute("height", bh);
    rect.setAttribute("fill", "#4ea1ff");
    rect.setAttribute("rx", "3");
    svg.appendChild(rect);
    const txt = document.createElementNS("http://www.w3.org/2000/svg", "text");
    txt.setAttribute("x", x + w / 2);
    txt.setAttribute("y", H - pad - bh - 5);
    txt.setAttribute("text-anchor", "middle");
    txt.setAttribute("font-size", "10");
    txt.textContent = v;
    svg.appendChild(txt);
    const lbl = document.createElementNS("http://www.w3.org/2000/svg", "text");
    lbl.setAttribute("x", x + w / 2);
    lbl.setAttribute("y", H - 2);
    lbl.setAttribute("text-anchor", "middle");
    lbl.setAttribute("font-size", "8");
    lbl.setAttribute("fill", "#9aa3b2");
    lbl.textContent = short(name);
    svg.appendChild(lbl);
  });
  el.appendChild(svg);
}

function short(url) {
  try {
    const u = new URL(url);
    return u.host;
  } catch {
    return url;
  }
}

$("send").addEventListener("click", send);
$("path").addEventListener("keydown", (e) => {
  if (e.key === "Enter") send();
});
