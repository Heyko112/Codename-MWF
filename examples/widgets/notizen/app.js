// Beispiel-Widget für die Daten-Brücke. Spricht die Tabelle über den Alias "notizen" an
// und die Spalten über ihre Schlüssel "text" und "erledigt".
"use strict";
const T = "notizen";
const statusEl = document.getElementById("status");
const form = document.getElementById("neu");
const input = document.getElementById("text");
const list = document.getElementById("liste");
let perms = {};

function status(msg, error) {
  statusEl.textContent = msg;
  statusEl.classList.toggle("error", !!error);
  statusEl.hidden = !msg;
}

function fail(e) {
  const details = e.fields ? Object.entries(e.fields).map(([k, v]) => k + ": " + v).join(", ") : "";
  status(e.message + (details ? " (" + details + ")" : ""), true);
}

function row(rec) {
  const li = document.createElement("li");
  li.classList.toggle("done", rec.values.erledigt === true);
  const box = document.createElement("input");
  box.type = "checkbox";
  box.checked = rec.values.erledigt === true;
  box.disabled = !perms.update;
  box.setAttribute("aria-label", "erledigt");
  box.addEventListener("change", async () => {
    try {
      const r = await mwf.update(T, rec.id, { erledigt: box.checked });
      li.replaceWith(row(r));
    } catch (e) { box.checked = !box.checked; fail(e); }
  });
  const text = document.createElement("span");
  text.textContent = rec.values.text;
  li.append(box, text);
  if (perms.delete) {
    const del = document.createElement("button");
    del.textContent = "Löschen";
    del.addEventListener("click", async () => {
      if (!confirm("Notiz löschen?")) return;
      try { await mwf.remove(T, rec.id); li.remove(); } catch (e) { fail(e); }
    });
    li.append(del);
  }
  return li;
}

async function load() {
  try {
    const t = (await mwf.tables()).find((x) => x.alias === T);
    if (!t || !t.mapped) { status("Noch keine Tabelle zugeordnet. Das macht ein Verwalter unter Widgets verwalten.", true); return; }
    perms = t.perms;
    if (!perms.read) { status("Du darfst die Notizen nicht sehen.", true); return; }
    form.hidden = !perms.create;
    const res = await mwf.list(T, { limit: 100 });
    list.replaceChildren(...res.records.map(row));
    status(res.total === 0 ? "Noch keine Notizen." : res.total > 100 ? "Die neuesten 100 von " + res.total + "." : "");
  } catch (e) { fail(e); }
}

form.addEventListener("submit", async (ev) => {
  ev.preventDefault();
  try {
    const rec = await mwf.create(T, { text: input.value, erledigt: false });
    list.prepend(row(rec));
    input.value = "";
    status("");
  } catch (e) { fail(e); }
});

load();
