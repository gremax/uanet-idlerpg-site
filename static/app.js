// rpg.uanet.net. The pages are complete without this script; it adds the
// switch between the two views, the copy buttons, timers that count down,
// and a fresh copy of the page every minute.
//
// A timer is <span data-left="523">: the seconds left when the server made
// the page. It counts from when the page arrived, by performance.now(), so
// the visitor's clock doesn't matter. At zero it waits for the bot: the page
// is fetched again every few seconds until the server has a new time.
'use strict';

(() => {
  const MODE_KEY = 'rpg-mode';
  const REFRESH = 60 * 1000;
  const RETRY_AT_ZERO = 10 * 1000;

  let loadedAt = performance.now();
  let refreshTimer;

  function plural(n, one, few, many) {
    const tens = n % 100;
    const units = n % 10;
    if (tens > 10 && tens < 20) return many;
    if (units === 1) return one;
    if (units > 1 && units < 5) return few;
    return many;
  }

  const two = (n) => String(n).padStart(2, '0');

  // "1 день, 02:03:04", as the server writes it
  function duration(seconds) {
    const s = Math.max(0, Math.floor(seconds));
    const days = Math.floor(s / 86400);
    return `${days} ${plural(days, 'день', 'дні', 'днів')}, ` +
      `${two(Math.floor(s % 86400 / 3600))}:${two(Math.floor(s % 3600 / 60))}:${two(s % 60)}`;
  }

  // Shows every timer; says whether one of them has run out.
  function tick() {
    const gone = (performance.now() - loadedAt) / 1000;
    let zero = false;
    for (const el of document.querySelectorAll('[data-left]')) {
      const left = Number(el.dataset.left) - gone;
      el.textContent = duration(left);
      if (left <= 0) zero = true;
    }
    return zero;
  }

  // Swaps the page for a fresh copy from the server, keeping the scroll.
  async function refresh() {
    clearTimeout(refreshTimer);
    try {
      const response = await fetch(location.href, { cache: 'no-store' });
      if (response.ok || response.status === 404) {
        const doc = new DOMParser().parseFromString(await response.text(), 'text/html');
        document.body.replaceWith(doc.body);
        document.title = doc.title;
        loadedAt = performance.now();
        setUp();
      }
    } catch {
      // offline for a moment: the page stays, the next try comes
    }
    schedule(tick() && !document.body.hasAttribute('data-stale') ? RETRY_AT_ZERO : REFRESH);
  }

  function schedule(ms) {
    clearTimeout(refreshTimer);
    refreshTimer = setTimeout(refresh, ms);
  }

  let toastTimer;
  function flash(message) {
    const toast = document.querySelector('.toast');
    toast.textContent = message;
    toast.hidden = false;
    clearTimeout(toastTimer);
    toastTimer = setTimeout(() => { toast.hidden = true; }, 1500);
  }

  // The buttons start hidden: without this script they'd do nothing, and copy
  // buttons need the clipboard API too.
  function setUp() {
    for (const button of document.querySelectorAll('[data-mode-toggle]')) button.hidden = false;
    if (navigator.clipboard && window.isSecureContext) {
      for (const button of document.querySelectorAll('[data-copy]')) button.hidden = false;
    }
  }

  document.addEventListener('click', (event) => {
    const toggle = event.target.closest('[data-mode-toggle]');
    if (toggle) {
      const root = document.documentElement;
      const mode = root.dataset.mode === 'term' ? 'paper' : 'term';
      root.dataset.mode = mode;
      try {
        localStorage.setItem(MODE_KEY, mode);
      } catch {
        // storage blocked: the choice holds until the next page
      }
      // the other view's button, now the visible one
      [...document.querySelectorAll('[data-mode-toggle]')].find((b) => b.offsetParent)?.focus();
      return;
    }
    const copy = event.target.closest('[data-copy]');
    if (copy) {
      const text = copy.dataset.copy;
      navigator.clipboard.writeText(text).then(() => flash(`Скопійовано: ${text}`), () => flash(text));
    }
  });

  setUp();
  let waiting = false;
  setInterval(() => {
    // the first time a timer reaches zero, ask for the page sooner
    const zero = tick();
    if (zero && !waiting && !document.body.hasAttribute('data-stale')) schedule(RETRY_AT_ZERO);
    waiting = zero;
  }, 1000);
  schedule(REFRESH);
})();
