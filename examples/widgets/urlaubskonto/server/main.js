// Server-Skript des Urlaubskontos. Läuft im MWF-Server, nicht im Browser.
// mwf.list & Co. arbeiten mit den Rechten des Benutzers, mwf.asService mit der Service-Rolle des Widgets.
const regeln = require("./regeln.js");

function genommen() {
  const r = mwf.list("urlaub", { where: [{ col: "person", op: "eq", value: "{ich}" }], limit: 200 });
  return r.records.reduce((summe, x) => summe + (x.values.tage || 0), 0);
}

exports.konto = function () {
  const jahr = mwf.settings().jahresurlaub || 30;
  const tage = genommen();
  return { jahresurlaub: jahr, genommen: tage, rest: jahr - tage };
};

exports.beantragen = function (p) {
  const tage = regeln.tagePruefen(p && p.tage);
  const konto = exports.konto();
  if (tage > konto.rest) {
    mwf.error("invalid", "Es sind nur noch " + konto.rest + " Tage übrig.");
  }
  // Der Benutzer selbst darf nicht anlegen, nur das Skript über die Service-Rolle.
  // So kann niemand an der Prüfung vorbei direkt in die Tabelle schreiben.
  const rec = mwf.asService.create("urlaub", {
    person: mwf.user.username,
    tage: tage,
    bemerkung: (p && p.bemerkung) || null,
  });
  console.log("Urlaub beantragt", mwf.user.username, tage);
  return { id: rec.id, rest: konto.rest - tage };
};
