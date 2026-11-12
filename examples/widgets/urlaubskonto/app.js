// Oberfläche des Urlaubskontos. Rechnen und Speichern macht das Server-Skript (server/main.js).
"use strict";
const kontoEl = document.getElementById("konto");
const statusEl = document.getElementById("status");
const form = document.getElementById("neu");
const list = document.getElementById("liste");

function status(msg, error) {
  statusEl.textContent = msg;
  statusEl.classList.toggle("error", !!error);
  statusEl.hidden = !msg;
}

async function laden() {
  const k = await mwf.run("konto");
  kontoEl.textContent = k.rest + " von " + k.jahresurlaub + " Tagen übrig (" + k.genommen + " genommen)";
  const { records } = await mwf.list("urlaub", { where: [{ col: "person", op: "eq", value: "{ich}" }], order: "-id" });
  list.replaceChildren(...records.map((r) => {
    const li = document.createElement("li");
    li.textContent = r.values.tage + " Tage" + (r.values.bemerkung ? " – " + r.values.bemerkung : "");
    return li;
  }));
  form.hidden = false;
}

form.addEventListener("submit", async (ev) => {
  ev.preventDefault();
  try {
    const r = await mwf.run("beantragen", { tage: Number(form.tage.value), bemerkung: form.bemerkung.value });
    status("Gespeichert. Noch " + r.rest + " Tage übrig.");
    form.reset();
    await laden();
  } catch (e) {
    status(e.message, true);
  }
});

laden().catch((e) => { kontoEl.textContent = e.message; kontoEl.classList.add("error"); });
