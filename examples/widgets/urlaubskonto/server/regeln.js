// Hilfsfunktionen, eingebunden mit require("./regeln.js")
exports.tagePruefen = function (wert) {
  const tage = Number(wert);
  if (!Number.isInteger(tage) || tage < 1 || tage > 60) {
    mwf.error("invalid", "Bitte eine ganze Zahl zwischen 1 und 60 angeben.");
  }
  return tage;
};
