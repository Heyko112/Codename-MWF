// mwf-widget.js: Bibliothek für Widgets. Einbinden mit <script src="/static/mwf-widget.js"></script>.
// Alle Aufrufe liefern ein Promise. Fehler kommen als mwf.MwfError mit code, message und fields.
// Typen für Editoren: /static/mwf-widget.d.ts
(function () {
  "use strict";
  if (window.mwf) return;
  var TIMEOUT = 15000;
  var seq = 0;
  var pending = {};

  function MwfError(code, message, fields) {
    var e = new Error(message);
    Object.setPrototypeOf(e, MwfError.prototype);
    e.name = "MwfError";
    e.code = code;
    e.fields = fields || {};
    return e;
  }
  MwfError.prototype = Object.create(Error.prototype);
  MwfError.prototype.constructor = MwfError;

  window.addEventListener("message", function (ev) {
    if (ev.source !== window.parent) return;
    var m = ev.data;
    if (!m || m.mwf !== 1 || !pending[m.id]) return;
    var p = pending[m.id];
    delete pending[m.id];
    clearTimeout(p.timer);
    if (m.ok) p.resolve(m.result);
    else {
      var e = m.error || {};
      p.reject(MwfError(e.code || "error", e.message || "Unbekannter Fehler.", e.fields));
    }
  });

  function call(method, params) {
    return new Promise(function (resolve, reject) {
      if (window.parent === window) {
        reject(MwfError("no_host", "Das Widget läuft nicht in MWF."));
        return;
      }
      var id = ++seq;
      var timer = setTimeout(function () {
        delete pending[id];
        reject(MwfError("timeout", "Keine Antwort vom Server."));
      }, TIMEOUT);
      pending[id] = { resolve: resolve, reject: reject, timer: timer };
      window.parent.postMessage({ mwf: 1, id: id, method: method, params: params || {} }, "*");
    });
  }

  function opts(table, o) {
    var p = { table: table };
    for (var k in o || {}) p[k] = o[k];
    return p;
  }

  window.mwf = {
    MwfError: MwfError,
    context: function () { return call("context"); },
    tables: function () { return call("tables"); },
    list: function (table, o) { return call("list", opts(table, o)); },
    get: function (table, id) { return call("get", { table: table, id: id }); },
    create: function (table, values) { return call("create", { table: table, values: values }); },
    update: function (table, id, values) { return call("update", { table: table, id: id, values: values }); },
    remove: function (table, id) { return call("delete", { table: table, id: id }); }
  };
})();
