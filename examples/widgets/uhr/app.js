"use strict";
const zeit = document.getElementById("zeit");
const datum = document.getElementById("datum");
const kw = document.getElementById("kw");
const titel = document.getElementById("titel");

// Vorgaben, falls die Einstellungen nicht geladen werden können
let cfg = { titel: "", zeitzone: "Europe/Berlin", sekunden: false, kw: true };

// ISO-Kalenderwoche für ein Datum aus Jahr, Monat, Tag
function woche(j, m, t) {
  const d = new Date(Date.UTC(j, m - 1, t));
  const tag = d.getUTCDay() || 7;
  d.setUTCDate(d.getUTCDate() + 4 - tag);
  const jahr = new Date(Date.UTC(d.getUTCFullYear(), 0, 1));
  return Math.ceil(((d - jahr) / 86400000 + 1) / 7);
}

function tick() {
  const d = new Date();
  let tz = cfg.zeitzone;
  try { new Intl.DateTimeFormat("de-DE", { timeZone: tz }); } catch (e) { tz = "Europe/Berlin"; }
  zeit.textContent = d.toLocaleTimeString("de-DE", { hour: "2-digit", minute: "2-digit", second: cfg.sekunden ? "2-digit" : undefined, timeZone: tz });
  datum.textContent = d.toLocaleDateString("de-DE", { weekday: "long", day: "numeric", month: "long", year: "numeric", timeZone: tz });
  const p = Object.fromEntries(new Intl.DateTimeFormat("en-CA", { timeZone: tz, year: "numeric", month: "numeric", day: "numeric" })
    .formatToParts(d).map((x) => [x.type, x.value]));
  kw.textContent = "KW " + woche(+p.year, +p.month, +p.day);
  kw.hidden = !cfg.kw;
}

mwf.settings().then((s) => {
  cfg = Object.assign(cfg, s);
  titel.textContent = cfg.titel || "";
  titel.hidden = !cfg.titel;
  tick();
}).catch(() => tick());

tick();
setInterval(tick, 1000);
