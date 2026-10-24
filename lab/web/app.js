// OpsForge Labs client: hash routes (#/, #/lab/<id>, #/badges), the live mimic panel, the lab workspace and its
// terminal. No build step; talks to lab/labserver.py.
"use strict";

const $ = (sel, root = document) => root.querySelector(sel);
const main = $("#main");
const POLL_MS = 2500;
const SVG = "http://www.w3.org/2000/svg";

let labState = null;      // last /api/state
let route = null;         // current view's re-render hook for state updates

// ---------- helpers ----------

async function api(path, body) {
  const opts = body === undefined ? {} : {
    method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(body),
  };
  const res = await fetch(path, opts);
  const data = await res.json().catch(() => ({ error: `${res.status} ${res.statusText}` }));
  if (!res.ok) throw new Error(data.error || `${res.status} ${res.statusText}`);
  return data;
}

function esc(s) {
  return String(s ?? "").replace(/[&<>"']/g, c => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c]));
}

function inline(s) {
  // Escape first, then a few inline forms: `code`, **bold**, [text](https://...)
  return esc(s)
    .replace(/`([^`]+)`/g, "<code>$1</code>")
    .replace(/\*\*([^*]+)\*\*/g, "<strong>$1</strong>")
    .replace(/\[([^\]]+)\]\((https?:\/\/[^)\s]+)\)/g, '<a href="$2" target="_blank" rel="noopener">$1</a>');
}

// The Markdown lab texts use: paragraphs, "- " lists, ``` fences (shell blocks get a paste button), inline forms.
function md(src) {
  const out = [];
  const lines = String(src || "").replace(/\r/g, "").split("\n");
  let para = [], list = [];
  const flush = () => {
    if (para.length) out.push(`<p>${inline(para.join(" "))}</p>`);
    if (list.length) out.push(`<ul>${list.map(i => `<li>${inline(i)}</li>`).join("")}</ul>`);
    para = []; list = [];
  };
  for (let i = 0; i < lines.length; i++) {
    const line = lines[i];
    const fence = line.match(/^\s*```(\w*)\s*$/);
    if (fence) {
      flush();
      const code = [];
      for (i++; i < lines.length && !/^\s*```\s*$/.test(lines[i]); i++) code.push(lines[i]);
      const text = code.join("\n");
      const shell = !fence[1] || /^(sh|bash|shell|console)$/.test(fence[1]);
      out.push(`<div class="cmd"><pre><code>${esc(text)}</code></pre>` +
        (shell ? `<button type="button" data-paste="${esc(text)}">Paste in terminal</button>` : "") + "</div>");
    } else if (/^\s*-\s+/.test(line)) {
      if (para.length) { out.push(`<p>${inline(para.join(" "))}</p>`); para = []; }
      list.push(line.replace(/^\s*-\s+/, ""));
    } else if (!line.trim()) {
      flush();
    } else {
      if (list.length && /^\s+/.test(line)) list[list.length - 1] += " " + line.trim();
      else { if (list.length) flush(); para.push(line.trim()); }
    }
  }
  flush();
  return out.join("");
}

let toastTimer;
function toast(text) {
  const el = $("#toast");
  el.textContent = text;
  el.classList.add("show");
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => el.classList.remove("show"), 5000);
}

function plural(n, word) { return `${n} ${word}${n === 1 ? "" : "s"}`; }

function duration(seconds) {
  const m = Math.floor(seconds / 60), s = Math.round(seconds % 60);
  return m ? `${m} min ${s} s` : `${s} s`;
}

// ---------- header and polling ----------

async function poll() {
  try {
    labState = await api("/api/state");
  } catch (e) {
    labState = null;
  }
  renderHeader();
  if (route && route.onState) route.onState();
}

function renderHeader() {
  const lamp = $(".bar .lamp"), text = $("#lab-state-text");
  if (!labState) {
    lamp.dataset.lamp = "off";
    text.textContent = "Server not reachable";
    return;
  }
  const lab = labState.lab;
  lamp.dataset.lamp = !lab.up ? "off" : lab.fault ? "red" : "white";
  text.textContent = !lab.up ? "Lab down" : lab.fault ? "Fault in place" : `Lab running, ${lab.layout} layout`;
  $("#xp").textContent = `${labState.user.xp} XP`;
}

function setNav(name) {
  document.querySelectorAll("[data-nav]").forEach(a => {
    if (a.dataset.nav === name) a.setAttribute("aria-current", "page");
    else a.removeAttribute("aria-current");
  });
}

// ---------- the mimic panel ----------

function el(tag, attrs = {}, parent) {
  const node = document.createElementNS(SVG, tag);
  for (const [k, v] of Object.entries(attrs)) node.setAttribute(k, v);
  if (parent) parent.appendChild(node);
  return node;
}

const W = 640, H = 330;

function panelLayout(lab) {
  const sites = Object.keys(lab.sites);
  const n = sites.length, cx = W / 2, cy = H / 2 + 6;
  const pos = {};
  sites.forEach((s, i) => {
    const a = (-150 + i * 360 / n) * Math.PI / 180;
    pos[s] = { x: cx + 225 * Math.cos(a), y: cy + 108 * Math.sin(a) };
  });
  return { pos, cx, cy };
}

function linkLamp(link) {
  return link.condition === "cut" ? "red" : link.condition === "changed" ? "amber" : "white";
}

class Panel {
  constructor(container) {
    this.root = document.createElement("div");
    this.root.className = "panel";
    this.signature = null;
    container.appendChild(this.root);
  }

  update(state) {
    const lab = state && state.lab;
    if (!lab || !Object.keys(lab.sites).length) {
      this.root.innerHTML = '<p class="panel-note">The panel appears once the server can read topology.yml.</p>';
      this.signature = null;
      return;
    }
    const signature = JSON.stringify([Object.keys(lab.sites), Object.keys(lab.links), lab.nodes.map(n => n.name)]);
    if (signature !== this.signature) this.build(lab);
    this.signature = signature;
    for (const [name, link] of Object.entries(lab.links)) {
      const g = this.links[name];
      const live = link.live, lamp = lab.up ? linkLamp(link) : "off";
      g.bulb.dataset.lamp = lamp;
      g.track.classList.toggle("cut", lab.up && link.condition === "cut");
      g.rtt.textContent = lab.up && link.condition === "cut" ? "cut" : `${live.rtt}`.replace("ms", " ms");
      g.was.textContent = lab.up && link.condition !== "ok" ? `set ${link.desired.rtt}`.replace("ms", " ms") : "";
      g.title.textContent = `${name}: ${describeLink(live)}` +
        (link.condition !== "ok" ? ` (topology.yml: ${describeLink(link.desired)})` : "");
    }
    for (const node of lab.nodes) {
      this.nodes[node.name].dataset.lamp = node.status === "Running" ? "white" : "off";
    }
    const note = this.root.querySelector(".panel-note");
    if (!lab.up) {
      note.textContent = "The lab is down: run ./opsforge up";
      note.classList.remove("alert");
    } else if (lab.fault) {
      note.textContent = "A fault is in place: opsforge heal restores the links";
      note.classList.add("alert");
    } else {
      note.textContent = lab.error || "";
      note.classList.toggle("alert", !!lab.error);
    }
  }

  build(lab) {
    this.root.innerHTML = "";
    const { pos, cx, cy } = panelLayout(lab);
    const svg = el("svg", { viewBox: `0 0 ${W} ${H}`, role: "img",
      "aria-label": `Lab map: sites ${Object.keys(lab.sites).join(", ")} and the WAN links between them` });
    this.links = {};
    this.nodes = {};
    const trackLayer = el("g", {}, svg), labelLayer = el("g", {}, svg), stationLayer = el("g", {}, svg);
    for (const name of Object.keys(lab.links)) {
      const [a, b] = name.split("-"), p = pos[a], q = pos[b];
      const g = el("g", {}, trackLayer);
      const title = el("title", {}, g);
      const track = el("line", { class: "track", x1: p.x, y1: p.y, x2: q.x, y2: q.y }, g);
      const mx = (p.x + q.x) / 2, my = (p.y + q.y) / 2;
      // Label on the outer side of the track, away from the panel's centre
      let nx = -(q.y - p.y), ny = q.x - p.x;
      const len = Math.hypot(nx, ny) || 1;
      nx /= len; ny /= len;
      if (nx * (mx - cx) + ny * (my - cy) < 0) { nx = -nx; ny = -ny; }
      const lg = el("g", {}, labelLayer);
      el("circle", { class: "plate", cx: mx, cy: my, r: 15 }, lg);
      const bulb = el("circle", { class: "bulb", cx: mx, cy: my, r: 9 }, lg);
      const lx = mx + nx * 34, ly = my + ny * 30;
      const rtt = el("text", { class: "rtt", x: lx, y: ly, "text-anchor": "middle", "dominant-baseline": "middle" }, lg);
      const was = el("text", { class: "rtt-was", x: lx, y: ly + 15, "text-anchor": "middle", "dominant-baseline": "middle" }, lg);
      this.links[name] = { track, bulb, rtt, was, title };
    }
    for (const [site, info] of Object.entries(lab.sites)) {
      const p = pos[site];
      const g = el("g", {}, stationLayer);
      el("rect", { class: "station", x: p.x - 34, y: p.y - 34, width: 68, height: 68, rx: 4 }, g);
      el("text", { class: "station-name", x: p.x, y: p.y + 2, "text-anchor": "middle", "dominant-baseline": "middle" }, g)
        .textContent = site;
      el("text", { class: "station-ip", x: p.x, y: p.y + 50, "text-anchor": "middle" }, g).textContent = info.public_ip;
      const side = p.x < cx - 20 ? -1 : 1;
      const nodes = lab.nodes.filter(n => n.site === site);
      nodes.forEach((node, i) => {
        const x = p.x + side * 50, y = p.y - (nodes.length - 1) * 9 + i * 18;
        const bulb = el("circle", { class: "bulb", cx: x, cy: y, r: 5 }, g);
        el("text", { class: "node-name", x: x + side * 11, y: y + 1, "text-anchor": side < 0 ? "end" : "start",
          "dominant-baseline": "middle" }, g).textContent = node.name;
        this.nodes[node.name] = bulb;
      });
    }
    this.root.appendChild(svg);
    const foot = document.createElement("div");
    foot.className = "panel-foot";
    foot.innerHTML = `
      <span class="legend"><span class="lamp" data-lamp="white"></span>As in topology.yml</span>
      <span class="legend"><span class="lamp" data-lamp="amber"></span>Changed live</span>
      <span class="legend"><span class="lamp" data-lamp="red"></span>Cut</span>
      <span class="panel-note"></span>`;
    this.root.appendChild(foot);
  }
}

function describeLink(l) {
  return ["rtt", "jitter", "loss", "rate", "mtu"].filter(k => k in l).map(k => `${k} ${l[k]}`).join(", ");
}

// ---------- catalog ----------

function labLamp(lab) {
  if (lab.status === "passed") return lab.stale ? "amber" : "green";
  if (lab.status === "running") return "white";
  return "off";
}

function labWhy(lab, titles) {
  if (lab.unavailable.length) return lab.unavailable.join("; ");
  if (lab.missing.length) return `Pass ${lab.missing.map(id => titles[id] || id).join(" and ")} first`;
  if (lab.locked) return "Unlocks when the level before it is passed";
  if (lab.status === "running") return "In progress";
  if (lab.stale) return "Updated since you passed it";
  return "";
}

async function showCatalog() {
  setNav("catalog");
  main.innerHTML = `
    <div class="catalog">
      <section class="intro">
        <div>
          <h1>Break the network. Watch the cloud cope.</h1>
          <p class="prose">Every lab runs on these three sites, live. Change a link or cut a region off,
          and the panel shows it as it happens.</p>
        </div>
        <div id="hero-panel"></div>
      </section>
      <div class="curricula" id="curricula"><p class="prose">Loading the labs</p></div>
    </div>`;
  const panel = new Panel($("#hero-panel"));
  panel.update(labState);
  route = { onState: () => panel.update(labState) };
  let data;
  try {
    data = await api("/api/catalog");
  } catch (e) {
    $("#curricula").innerHTML = `<p class="notice alert">Could not load the labs: ${esc(e.message)}</p>`;
    return;
  }
  const titles = {};
  data.curricula.forEach(c => c.levels.forEach(l => l.labs.forEach(lab => { titles[lab.id] = lab.title; })));
  if (!data.curricula.length) {
    $("#curricula").innerHTML = '<p class="prose">No labs yet. Add a pack under labs/ and run ./opsforge labs import.</p>';
    return;
  }
  $("#curricula").innerHTML = data.curricula.map(c => `
    <section class="curriculum" aria-labelledby="cur-${esc(c.id)}">
      <h2 id="cur-${esc(c.id)}">${esc(c.title)}</h2>
      <div class="prose">${md(c.summary)}</div>
      ${c.levels.map((level, i) => `
        <div class="level">
          <h3><span class="num">Level ${i + 1}</span> ${esc(level.title)}</h3>
          <p class="level-note">${level.unlocked ? esc(level.summary || "") :
            level.xp_required != null ? `Unlocks at ${level.xp_required} XP` : "Unlocks when level " + i + " is passed"}</p>
          <ol class="lab-list">
            ${level.labs.map(lab => {
              const why = labWhy(lab, titles);
              const cls = lab.locked ? "locked" : lab.unavailable.length ? "unavailable" : "";
              return `<li class="lab-item ${cls}">
                <a href="#/lab/${esc(lab.id)}">
                  <span class="lamp" data-lamp="${labLamp(lab)}" aria-hidden="true"></span>
                  <span>
                    <span class="lab-title">${esc(lab.title)}</span>
                    <span class="lab-summary">${esc(lab.summary)}</span>
                    ${why ? `<span class="lab-why ${lab.stale ? "stale" : ""}">${esc(why)}</span>` : ""}
                  </span>
                  <span class="lab-meta">${lab.status === "passed" ? `${lab.best_xp} of ${lab.xp} XP` : `${lab.xp} XP`}
                    ${lab.minutes ? `<br>${lab.minutes} min` : ""}</span>
                </a>
              </li>`;
            }).join("")}
          </ol>
        </div>`).join("")}
    </section>`).join("");
}

// ---------- terminal (one shell for the whole session; moved between views) ----------

const term = {
  xterm: null, fit: null, ws: null, host: null, status: null,

  mount(container) {
    if (!window.Terminal) {
      container.innerHTML = '<p class="term-head">The terminal needs xterm.js from cdn.jsdelivr.net. Use your own terminal: commands start with ./opsforge.</p>';
      return;
    }
    if (!this.xterm) {
      this.host = document.createElement("div");
      this.host.style.height = "100%";
      this.xterm = new Terminal({
        fontFamily: '"JetBrains Mono", ui-monospace, monospace', fontSize: 13, cursorBlink: true, scrollback: 5000,
        theme: { background: getComputedStyle(document.documentElement).getPropertyValue("--term-bg").trim() || "#1E2925",
          foreground: "#E4E7D9", cursor: "#F6F3D8", selectionBackground: "#4A5953" },
      });
      this.fit = new FitAddon.FitAddon();
      this.xterm.loadAddon(this.fit);
      this.xterm.onData(d => this.send({ t: "i", d }));
      this.xterm.onResize(({ cols, rows }) => this.send({ t: "r", cols, rows }));
      new ResizeObserver(() => { if (this.host.isConnected) this.fit.fit(); }).observe(this.host);
    }
    container.appendChild(this.host);
    if (!this.xterm.element) this.xterm.open(this.host);
    requestAnimationFrame(() => this.fit.fit());
    if (!this.ws || this.ws.readyState > 1) this.connect();
  },

  connect() {
    const ws = new WebSocket(`ws://${location.host}/ws/terminal`);
    ws.binaryType = "arraybuffer";
    ws.onopen = () => {
      this.setStatus("Connected");
      this.send({ t: "r", cols: this.xterm.cols, rows: this.xterm.rows });
    };
    ws.onmessage = e => this.xterm.write(new Uint8Array(e.data));
    ws.onclose = () => {
      this.setStatus("Disconnected", true);
      this.xterm.write("\r\n[terminal closed: use Reconnect to start a new shell]\r\n");
    };
    this.ws = ws;
  },

  send(msg) {
    if (this.ws && this.ws.readyState === 1) this.ws.send(JSON.stringify(msg));
  },

  paste(text) {
    if (!this.xterm) return;
    this.xterm.paste(text);
    this.xterm.focus();
  },

  setStatus(text, closed) {
    const el = $("#term-status");
    if (!el) return;
    el.innerHTML = closed ? `${esc(text)} <button type="button" class="link-btn" id="term-reconnect">Reconnect</button>` : esc(text);
    const btn = $("#term-reconnect");
    if (btn) btn.onclick = () => { this.xterm.reset(); this.connect(); };
  },
};

document.addEventListener("click", e => {
  const btn = e.target.closest("[data-paste]");
  if (!btn) return;
  if (term.xterm) term.paste(btn.dataset.paste);
  else navigator.clipboard?.writeText(btn.dataset.paste).then(() => toast("Copied"));
});

// ---------- lab workspace ----------

async function showLab(id) {
  setNav("catalog");
  main.innerHTML = `
    <div class="workspace">
      <div class="steps-col" id="steps-col"><p class="prose">Loading the lab</p></div>
      <div class="right-col">
        <div id="lab-panel"></div>
        <div class="term-wrap">
          <div class="term-head"><span>Terminal on this host, in the repo</span><span id="term-status"></span></div>
          <div id="terminal"></div>
        </div>
      </div>
    </div>`;
  const panel = new Panel($("#lab-panel"));
  panel.update(labState);
  term.mount($("#terminal"));
  if (term.ws) term.setStatus(term.ws.readyState === 1 ? "Connected" : "Connecting", term.ws.readyState > 1);
  const view = { lab: null, finished: null, results: {}, busy: null };
  route = { onState: () => { panel.update(labState); if (view.lab && !view.lab.attempt) renderLab(view); } };
  try {
    view.lab = await api(`/api/labs/${encodeURIComponent(id)}`);
  } catch (e) {
    $("#steps-col").innerHTML = `<a class="back" href="#/">All labs</a><div class="empty"><h1>Lab not found</h1>
      <p>${esc(e.message)}</p></div>`;
    return;
  }
  renderLab(view);
}

function blockers(lab) {
  const out = [];
  if (lab.locked) out.push("This lab is locked: pass the labs before it first.");
  for (const u of lab.unavailable) out.push(`This lab ${u}. Switch with OPSFORGE_TOPOLOGY, then up and provision.`);
  if (labState && !labState.lab.up) out.push("The lab is down. Run ./opsforge up (and provision) in the terminal.");
  if (labState && labState.running && labState.running.lab !== lab.id) {
    out.push(`Another lab is running. Finish or abandon it first.`);
  }
  return out;
}

function renderLab(view) {
  const lab = view.lab, col = $("#steps-col");
  if (!col) return;
  const running = !!lab.attempt;
  const currentIdx = lab.steps.findIndex(s => !s.passed);
  const facts = [lab.minutes && `${lab.minutes} minutes`, `${lab.xp} XP`,
    lab.passed && `best pass ${lab.best_xp} XP`].filter(Boolean).join(", ");
  const blocks = running || view.finished ? [] : blockers(lab);
  let html = `
    <a class="back" href="#/">All labs</a>
    <h1>${esc(lab.title)}</h1>
    <p class="lab-facts">${esc(facts)}${lab.concepts.length ? `. Covers ${esc(lab.concepts.join(", "))}.` : ""}</p>
    <div class="prose">${md(lab.briefing)}</div>`;
  if (!running && !view.finished) {
    html += blocks.map(b => `<p class="notice">${esc(b)}</p>`).join("");
    html += `<div class="start-row">
      <button class="btn" id="start" ${blocks.length || view.busy ? "disabled" : ""}>
        ${view.busy === "start" ? "Setting up the lab" : lab.passed ? "Start again" : "Start lab"}</button>
      <span class="why">${plural(lab.steps.length, "step")}. Starting may prepare the lab first; finishing undoes it.</span>
    </div>`;
  }
  html += `<ol class="steps">`;
  lab.steps.forEach((step, i) => {
    const state = view.finished || step.passed ? "passed" : running && i === currentIdx ? "current" : "ahead";
    html += `<li class="step ${state}" id="step-${esc(step.id)}">
      <span class="step-num" aria-hidden="true">${state === "passed" ? "✓" : i + 1}</span>
      <div><h2><span class="visually-hidden">Step ${i + 1}: </span>${esc(step.title)}</h2>`;
    if (state === "current") html += currentStep(view, step);
    else if (state === "passed" && view.results[step.id]) html += resultHtml(view.results[step.id]);
    html += `</div></li>`;
  });
  html += `</ol>`;
  if (view.finished) html += debriefHtml(lab, view.finished);
  if (running) {
    html += `<p class="abandon-row"><button class="link-btn" id="abandon" ${view.busy ? "disabled" : ""}>Abandon lab</button>
      <span class="why">Undoes the lab's setup and heals the links, without a pass.</span></p>`;
  }
  col.innerHTML = html;
  wireLab(view);
}

function currentStep(view, step) {
  const lab = view.lab, busy = view.busy === step.id;
  let html = `<div class="prose">${md(step.body)}</div>`;
  html += step.hints.map((h, i) => {
    const body = md(h), label = `<strong>Hint ${i + 1}.</strong> `;
    return `<div class="hint prose">${body.startsWith("<p>") ? body.replace("<p>", `<p>${label}`) : `<p>${label}</p>${body}`}</div>`;
  }).join("");
  const check = step.check;
  html += `<form class="check" data-step="${esc(step.id)}">`;
  if (check.type === "quiz") {
    html += `<div class="quiz"><fieldset><legend>${esc(check.question)}</legend>
      ${check.choices.map((c, i) => `<label><input type="radio" name="answer" value="${i}" required> <span>${inline(c)}</span></label>`).join("")}
      </fieldset></div>`;
  }
  const label = check.type === "quiz" ? "Answer" : check.type === "confirm" ? esc(check.label) :
    busy ? "Checking the lab" : "Check my work";
  html += `<div class="check-actions"><button class="btn" type="submit" ${view.busy ? "disabled" : ""}>${label}</button>`;
  if (step.hints.length < step.hints_total) {
    const cost = Math.round(lab.xp * lab.hint_penalty);
    html += `<button class="link-btn" type="button" data-hint="${esc(step.id)}">Show hint ${step.hints.length + 1} of
      ${step.hints_total}</button><span class="why">costs ${cost} XP</span>`;
  }
  html += `</div></form>`;
  if (busy && check.type === "probe") html += `<p class="lab-facts">Reading the live lab. A check that waits for the cluster can take a couple of minutes.</p>`;
  if (view.results[step.id]) html += resultHtml(view.results[step.id]);
  return html;
}

function resultHtml(r) {
  if (r.kind === "quiz") {
    return r.passed ? `<div class="result pass"><p><strong>Right.</strong> ${inline(r.explain || "")}</p></div>`
      : `<div class="result"><p><strong>Not that one.</strong> Read the step again and try another answer.</p></div>`;
  }
  if (r.kind === "confirm") return "";
  const rows = (r.results || []).map(x => `<tr><td>${esc(x.metric)}</td><td>wanted ${esc(x.want)}</td>
    <td>${x.checked === false ? "not checked" : `found ${x.got == null ? "nothing" : esc(typeof x.got === "number" ? +x.got.toFixed(2) : x.got)}`}</td>
    <td>${x.passed ? "pass" : x.checked === false ? "" : "fail"}</td></tr>`).join("");
  return `<div class="result ${r.passed ? "pass" : ""}">
    <p><strong>${r.passed ? "Passed." : "Not yet."}</strong> ${r.passed ? "The lab is in the state this step asks for."
      : r.error ? `The check could not finish: ${esc(r.error)}` : "The lab is not in the state this step asks for yet."}</p>
    ${rows ? `<table>${rows}</table>` : ""}
    ${r.log && r.log.length ? `<details><summary>What the check ran</summary><pre>${esc(r.log.join("\n"))}</pre></details>` : ""}
  </div>`;
}

function debriefHtml(lab, f) {
  return `<section class="debrief" aria-live="polite">
    <h2>Lab passed</h2>
    <p>${f.xp} XP${f.seconds ? ` in ${duration(f.seconds)}` : ""}. The lab has been put back as it was.</p>
    ${lab.debrief ? `<div class="prose">${md(lab.debrief)}</div>` : ""}
    ${f.teardown_error ? `<p class="notice alert">Undoing the setup failed: ${esc(f.teardown_error)}</p>` : ""}
    ${f.badges.length ? `<div class="awarded">${f.badges.map(b => badgeHtml(b, true)).join("")}</div>` : ""}
    <p><a href="#/">Back to all labs</a></p>
  </section>`;
}

function wireLab(view) {
  const lab = view.lab;
  const start = $("#start");
  if (start) start.onclick = async () => {
    view.busy = "start"; renderLab(view);
    try {
      view.lab = await api(`/api/labs/${encodeURIComponent(lab.id)}/start`, {});
      view.finished = null; view.results = {};
      poll();
    } catch (e) { toast(e.message); }
    view.busy = null; renderLab(view);
  };
  const abandon = $("#abandon");
  if (abandon) abandon.onclick = async () => {
    if (!confirm("Abandon this lab? Its setup is undone and the links are healed. Nothing is passed.")) return;
    view.busy = "abandon"; renderLab(view);
    try {
      await api(`/api/attempts/${lab.attempt.id}/abandon`, {});
      view.lab = await api(`/api/labs/${encodeURIComponent(lab.id)}`);
      view.results = {};
      toast("Lab abandoned. The links are healed.");
      poll();
    } catch (e) { toast(e.message); }
    view.busy = null; renderLab(view);
  };
  document.querySelectorAll("[data-hint]").forEach(btn => btn.onclick = async () => {
    try {
      const r = await api(`/api/attempts/${lab.attempt.id}/hint`, { step: btn.dataset.hint });
      const step = lab.steps.find(s => s.id === btn.dataset.hint);
      step.hints.push(r.hint);
      lab.attempt.hints_used = r.hints_used;
      renderLab(view);
    } catch (e) { toast(e.message); }
  });
  const form = $("form.check");
  if (form) form.onsubmit = async e => {
    e.preventDefault();
    const stepId = form.dataset.step, step = lab.steps.find(s => s.id === stepId);
    const body = { step: stepId };
    if (step.check.type === "quiz") {
      const picked = form.querySelector("input[name=answer]:checked");
      if (!picked) return;
      body.answer = Number(picked.value);
    }
    view.busy = stepId; renderLab(view);
    try {
      const r = await api(`/api/attempts/${lab.attempt.id}/check`, body);
      view.results[stepId] = { ...r, kind: step.check.type };
      if (r.passed) step.passed = true;
      if (r.finished) {
        view.finished = r.finished;
        lab.attempt = null; lab.passed = true; lab.best_xp = Math.max(lab.best_xp, r.finished.xp);
        r.finished.badges.forEach(b => toast(`Badge: ${b.title}`));
        poll();
      }
    } catch (err) { toast(err.message); }
    view.busy = null; renderLab(view);
    const next = $(".step.current") || $(".debrief");
    if (next && view.results[stepId] && view.results[stepId].passed) next.scrollIntoView({ block: "nearest", behavior: matchMedia("(prefers-reduced-motion: reduce)").matches ? "auto" : "smooth" });
  };
}

// ---------- badges ----------

const ICONS = {
  lamp: '<circle class="plate-lamp" cx="32" cy="32" r="10"/>',
  signal: '<path class="plate-icon" d="M32 50V22"/><circle class="plate-lamp" cx="32" cy="20" r="7"/><path class="plate-icon" d="M24 50h16"/>',
  points: '<path class="plate-icon" d="M16 44h32M16 44c12 0 18-6 32-20"/><circle class="plate-lamp" cx="48" cy="24" r="5"/>',
  token: '<circle class="plate-icon" cx="32" cy="32" r="13"/><circle class="plate-lamp" cx="32" cy="32" r="5"/>',
  route: '<path class="plate-icon" d="M14 42h12l12-20h12"/><circle class="plate-lamp" cx="50" cy="22" r="5"/>',
  bell: '<path class="plate-icon" d="M22 42c0-14 3-20 10-20s10 6 10 20zM18 42h28M32 46v3"/><circle class="plate-lamp" cx="32" cy="18" r="4"/>',
};

function badgeHtml(b, earned) {
  const when = b.awarded_at ? new Date(b.awarded_at).toLocaleDateString(undefined, { day: "numeric", month: "long", year: "numeric" }) : "";
  return `<div class="badge ${earned ? "earned" : ""}">
    <svg viewBox="0 0 64 64" aria-hidden="true"><rect class="plate-bg" x="3" y="3" width="58" height="58" rx="29"/>${ICONS[b.icon] || ICONS.lamp}</svg>
    <div><h2>${esc(b.title)}</h2><p>${esc(b.description)}</p>${when ? `<p class="when">Earned ${esc(when)}</p>` : ""}</div>
  </div>`;
}

async function showBadges() {
  setNav("badges");
  route = null;
  main.innerHTML = `<div class="badges-page"><h1>Badges</h1><p>Loading</p></div>`;
  try {
    const { badges } = await api("/api/badges");
    const earned = badges.filter(b => b.awarded_at).length;
    main.innerHTML = `<div class="badges-page">
      <h1>Badges</h1>
      <p>${earned} of ${badges.length} earned. Badges come from what you have passed, so a new badge can arrive for
      labs you passed before it existed.</p>
      <ul class="badge-grid">${badges.map(b => `<li>${badgeHtml(b, !!b.awarded_at)}</li>`).join("")}</ul>
    </div>`;
  } catch (e) {
    main.innerHTML = `<div class="badges-page"><h1>Badges</h1><p class="notice alert">${esc(e.message)}</p></div>`;
  }
}

// ---------- router ----------

function router() {
  const hash = location.hash || "#/";
  const lab = hash.match(/^#\/lab\/([a-z0-9-]+)$/);
  if (lab) showLab(lab[1]);
  else if (hash === "#/badges") showBadges();
  else showCatalog();
  main.focus({ preventScroll: true });
  window.scrollTo(0, 0);
}

window.addEventListener("hashchange", router);
poll().then(router);
setInterval(poll, POLL_MS);
