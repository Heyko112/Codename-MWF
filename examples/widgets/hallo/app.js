// Beispiel-Widget „Hallo“: Uhr, Zähler, Dialog und ein Selbsttest der Abschottung.
(function () {
  const clock = document.getElementById('clock');
  const tick = () => { clock.textContent = new Date().toLocaleTimeString('de-DE'); };
  tick();
  setInterval(tick, 1000);

  let n = 0;
  const count = document.getElementById('count');
  count.addEventListener('click', () => { n++; count.textContent = 'Klicks: ' + n; });

  const dlg = document.getElementById('dlg');
  document.getElementById('open').addEventListener('click', () => dlg.showModal());

  const checks = document.getElementById('checks');
  const report = (ok, text) => {
    const li = document.createElement('li');
    li.className = ok ? 'ok' : 'bad';
    li.textContent = text;
    checks.appendChild(li);
  };

  // Ohne allow-same-origin hat der Rahmen eine leere Herkunft: kein Zugriff auf Cookies
  try {
    const c = document.cookie;
    report(c === '', c === '' ? 'Keine Cookies sichtbar' : 'Cookies sichtbar!');
  } catch (e) {
    report(true, 'Kein Zugriff auf Cookies');
  }

  // Kein Zugriff auf die MWF-Seite außen herum
  try {
    const t = window.parent.document.title;
    report(false, 'Zugriff auf die MWF-Seite möglich: ' + t);
  } catch (e) {
    report(true, 'Kein Zugriff auf die MWF-Seite');
  }

  // Kein Internet: die CSP blockiert fremde Adressen
  fetch('https://example.com/', { mode: 'no-cors' })
    .then(() => report(false, 'Internet erreichbar!'))
    .catch(() => report(true, 'Internet gesperrt'));
})();
