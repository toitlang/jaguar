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

// A frame is "opaque" when we're paused in it but its source could not be
// loaded — the /source fetch for the current file 404'd, leaving sourceLines
// null. SDK and package frames usually DO resolve now (against the SDK lib and
// package.lock), so this is keyed on the actual fetch result, not the path.
function frameHasNoSource() {
  const loc = state.location;
  if (!loc || !loc.file) return false;
  return loc.file === sourceFile && !sourceLines;
}

function renderBanner() {
  const b = document.getElementById("banner");
  if (state.status === "paused" && frameHasNoSource()) {
    const loc = state.location;
    b.hidden = false;
    b.innerHTML = "";
    const lead = document.createTextNode("⚠ Paused in library code with no source — ");
    const where = document.createElement("b");
    where.textContent = `${loc.file}:${loc.line} (${loc.method})`;
    const tail = document.createTextNode(
      ". This is not an exception. Press ⤴ Out to return to your code, or ▶ Continue to run on.");
    b.append(lead, where, tail);
  } else {
    b.hidden = true;
    b.textContent = "";
  }
}

function renderHeader() {
  const s = document.getElementById("status");
  s.textContent = state.status; s.className = "status " + state.status;
  const loc = document.getElementById("location");
  loc.textContent = state.location
    ? `${state.location.file}:${state.location.line}  (${state.location.method})` : "";
  // Disable the resume controls once the program is finished: "done" (ran to
  // completion) or "exited" (VM gone). There's nothing left to step into.
  const finished = state.status === "done" || state.status === "exited";
  for (const id of ["btn-continue", "btn-in", "btn-over", "btn-out"])
    document.getElementById(id).disabled = finished;
}

async function apply(update) {
  state = update;
  renderHeader(); renderVars();
  // Prefer the current paused location's file; otherwise fall back to the
  // program's entrypoint so source (and clickable gutter breakpoints) are
  // available even at the entry stub, before any breakpoint is hit.
  const showFile = (state.location && state.location.file) || state.entry_file || null;
  if (showFile && showFile !== sourceFile) await loadSource(showFile);
  else renderSource();
  renderBanner();
}

document.getElementById("btn-continue").onclick = () => postCmd({ verb: "continue" });
document.getElementById("btn-in").onclick = () => postCmd({ verb: "step" });
document.getElementById("btn-over").onclick = () => postCmd({ verb: "over" });
document.getElementById("btn-out").onclick = () => postCmd({ verb: "out" });

const events = new EventSource("/events");
events.onmessage = (e) => apply(JSON.parse(e.data));
