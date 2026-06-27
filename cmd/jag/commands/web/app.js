// jag debug web UI: subscribes to /events (SSE), renders source + current line
// + gutter breakpoints + variables, and POSTs commands to /cmd.
let state = { status: "running", location: null, breakpoints: [], variables: [], method_id: 0 };
let sourceFile = null;
let sourceLines = [];

async function postCmd(body) {
  const notice = document.getElementById("notice");
  notice.textContent = "";
  const res = await fetch("/cmd", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });
  if (!res.ok) notice.textContent = await res.text();
}

function bpSet(file, line) {
  return state.breakpoints.some(b => b.file === file && b.line === line);
}

function toggleBreakpoint(line) {
  if (!sourceFile) return;
  postCmd({ verb: bpSet(sourceFile, line) ? "clear" : "break", file: sourceFile, line });
}

async function loadSource(file) {
  const res = await fetch("/source?file=" + encodeURIComponent(file));
  sourceFile = file;
  sourceLines = res.ok ? (await res.text()).split("\n") : null;
  renderSource();
}

function renderSource() {
  const el = document.getElementById("source");
  el.innerHTML = "";
  if (!sourceLines) { el.textContent = "source unavailable for " + sourceFile; return; }
  const cur = state.location && state.location.file === sourceFile ? state.location.line : -1;
  sourceLines.forEach((text, i) => {
    const n = i + 1;
    const row = document.createElement("div");
    row.className = "line" + (n === cur ? " current" : "") + (bpSet(sourceFile, n) ? " bp" : "");
    const g = document.createElement("span");
    g.className = "gutter"; g.textContent = n;
    g.onclick = () => toggleBreakpoint(n);
    const c = document.createElement("span");
    c.className = "code"; c.textContent = text;
    row.append(g, c); el.append(row);
  });
}

function renderVars() {
  const tbody = document.querySelector("#vars tbody");
  tbody.innerHTML = "";
  for (const v of state.variables) {
    const tr = document.createElement("tr");
    const k = document.createElement("td"); k.textContent = "r" + v.slot;
    const val = document.createElement("td"); val.textContent = v.value;
    tr.append(k, val); tbody.append(tr);
  }
}

function renderHeader() {
  const s = document.getElementById("status");
  s.textContent = state.status; s.className = "status " + state.status;
  const loc = document.getElementById("location");
  loc.textContent = state.location
    ? `${state.location.file}:${state.location.line}  (${state.location.method})` : "";
  const running = state.status !== "paused";
  for (const id of ["btn-continue", "btn-step", "btn-over", "btn-out"])
    document.getElementById(id).disabled = state.status === "exited" || running;
}

async function apply(update) {
  state = update;
  renderHeader(); renderVars();
  if (state.location && state.location.file !== sourceFile) await loadSource(state.location.file);
  else renderSource();
}

document.getElementById("btn-continue").onclick = () => postCmd({ verb: "continue" });
document.getElementById("btn-step").onclick = () => postCmd({ verb: "step" });
document.getElementById("btn-over").onclick = () => postCmd({ verb: "over" });
document.getElementById("btn-out").onclick = () => postCmd({ verb: "out" });

const events = new EventSource("/events");
events.onmessage = (e) => apply(JSON.parse(e.data));
