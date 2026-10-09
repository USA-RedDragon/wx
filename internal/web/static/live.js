(function () {
  'use strict';
  if (!window.EventSource || !window.fetch || !window.DOMParser) {
    return;
  }

  const widgets = ['current', 'sunmoon', 'hilo', 'sensors', 'statistics', 'celestial', 'identifier'];
  const plotInterval = 60000;
  let latest = 0;
  let shown = 0;
  let lastPlots = 0;
  let pending = false;

  function withStamp(src, stamp) {
    const base = src.split('?')[0];
    return base + '?nocache=' + stamp;
  }

  function refreshPlots(stamp, force) {
    const now = Date.now();
    if (!force && now - lastPlots < plotInterval) {
      return;
    }
    lastPlots = now;
    const images = document.querySelectorAll('img[src*=".svg"]');
    for (let i = 0; i < images.length; i++) {
      const img = images[i];
      if (img.offsetParent === null) {
        img.dataset.stamp = stamp;
        continue;
      }
      img.src = withStamp(img.getAttribute('src'), stamp);
      delete img.dataset.stamp;
    }
  }

  function applyStale() {
    const images = document.querySelectorAll('img[data-stamp]');
    for (let i = 0; i < images.length; i++) {
      const img = images[i];
      if (img.offsetParent !== null) {
        img.src = withStamp(img.getAttribute('src'), img.dataset.stamp);
        delete img.dataset.stamp;
      }
    }
  }

  function swap(doc) {
    const title = document.querySelector('#title .lastupdate');
    const newTitle = doc.querySelector('#title .lastupdate');
    if (title && newTitle) {
      title.textContent = newTitle.textContent;
    }
    for (let i = 0; i < widgets.length; i++) {
      const id = widgets[i] + '_widget';
      const cur = document.querySelector('#' + id + ' .widget_contents');
      const next = doc.querySelector('#' + id + ' .widget_contents');
      if (cur && next) {
        cur.innerHTML = next.innerHTML;
      }
    }
    if (typeof choose_history === 'function' && typeof get_active_div === 'function') {
      choose_history(get_active_div('history', ['day', 'week', 'month', 'year'], 'day'));
    }
  }

  function refresh() {
    if (document.hidden) {
      pending = true;
      return;
    }
    pending = false;
    if (latest <= shown) {
      return;
    }
    const stamp = latest;
    fetch(window.location.pathname + '?nocache=' + stamp, { cache: 'no-store' })
      .then(function (r) { return r.ok ? r.text() : Promise.reject(r.status); })
      .then(function (text) {
        swap(new DOMParser().parseFromString(text, 'text/html'));
        shown = stamp;
        refreshPlots(stamp, false);
      })
      .catch(function () {});
  }

  if (typeof choose_history === 'function') {
    const original = choose_history;
    window.choose_history = function (id) {
      original(id);
      applyStale();
    };
  }

  document.addEventListener('visibilitychange', function () {
    if (!document.hidden && pending) {
      refresh();
    }
  });

  const source = new EventSource('events');
  source.addEventListener('record', function (e) {
    const data = JSON.parse(e.data);
    if (data.dateTime > latest) {
      latest = data.dateTime;
    }
    refresh();
  });
  source.addEventListener('current', function (e) {
    if (document.hidden) {
      return;
    }
    const data = JSON.parse(e.data);
    const body = document.querySelector('#current_widget tbody');
    if (body && data.current) {
      body.innerHTML = data.current;
    }
  });
})();
