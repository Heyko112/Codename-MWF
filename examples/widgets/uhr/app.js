"use strict";
const zeit = document.getElementById("zeit");
const datum = document.getElementById("datum");
const kw = document.getElementById("kw");

// ISO-Kalenderwoche
function woche(d) {
  const t = new Date(Date.UTC(d.getFullYear(), d.getMonth(), d.getDate()));
  const tag = t.getUTCDay() || 7;
  t.setUTCDate(t.getUTCDate() + 4 - tag);
  const jahr = new Date(Date.UTC(t.getUTCFullYear(), 0, 1));
  return Math.ceil(((t - jahr) / 86400000 + 1) / 7);
}

function tick() {
  const d = new Date();
  zeit.textContent = d.toLocaleTimeString("de-DE", { hour: "2-digit", minute: "2-digit" });
  datum.textContent = d.toLocaleDateString("de-DE", { weekday: "long", day: "numeric", month: "long", year: "numeric" });
  kw.textContent = "KW " + woche(d);
}
tick();
setInterval(tick, 10000);
