// widget-host.js läuft auf der MWF-Seite um den Widget-Rahmen.
// Es nimmt Anfragen des Widgets per postMessage an und reicht sie mit der Sitzung des Benutzers an die API weiter.
// Das Widget selbst hat keine Cookies und kommt so nur an das, was Benutzer und Freigabe erlauben.
(function () {
  "use strict";
  var MAX_PENDING = 20;
  var pending = 0;

  function frame() { return document.querySelector("iframe.widget-frame"); }

  window.addEventListener("message", function (ev) {
    var f = frame();
    // Nur Nachrichten aus dem eigenen Widget-Rahmen (sandbox ohne allow-same-origin hat die Herkunft "null")
    if (!f || ev.source !== f.contentWindow || ev.origin !== "null") return;
    var m = ev.data;
    if (!m || m.mwf !== 1 || typeof m.method !== "string" || (typeof m.id !== "number" && typeof m.id !== "string")) return;
    var target = ev.source;
    function reply(r) {
      r.mwf = 1;
      r.id = m.id;
      target.postMessage(r, "*");
    }
    if (pending >= MAX_PENDING) {
      reply({ ok: false, error: { code: "busy", message: "Zu viele gleichzeitige Anfragen." } });
      return;
    }
    pending++;
    fetch(f.dataset.api, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ method: m.method, params: m.params || {} }),
      credentials: "same-origin",
      redirect: "manual"
    }).then(function (res) {
      var ct = res.headers.get("Content-Type") || "";
      if (res.type === "opaqueredirect" || ct.indexOf("application/json") !== 0) {
        return { ok: false, error: { code: "session", message: "Sitzung abgelaufen. Bitte die Seite neu laden und anmelden." } };
      }
      return res.json();
    }).catch(function () {
      return { ok: false, error: { code: "network", message: "Server nicht erreichbar." } };
    }).then(function (r) {
      pending--;
      reply(r);
    });
  });
})();
