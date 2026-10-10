// The page anonymizes itself as you scroll.
//
// A field of drifting data fragments sits behind the content. At the top of the page
// they are legible personal data drawn in the PII colour. Each one carries its own
// depth window: as the scroll crosses it, redaction bars in the masked colour cover the
// fragment one segment after another, until a single bar is left. By the bottom of the
// page the field is entirely redacted, and the gutter readout reports the real
// proportion — it counts covered characters, it does not fake a number.
//
// The background gradient (mask.css) does the rest, with no JS at all.
(function () {
  var canvas = document.querySelector('.mask-field');
  if (!canvas || !canvas.getContext) return;
  var ctx = canvas.getContext('2d');
  var reduce = window.matchMedia('(prefers-reduced-motion: reduce)').matches;

  // Density curve: the field is already there at the top of the page, at FIELD_MIN so
  // the data in the clear can be read, then rises gently to the FIELD_MAX ceiling.
  var FIELD_MIN = 0.4;
  var FIELD_MAX = 0.55;
  var TEXT_ALPHA = 0.75;
  // Nothing is redacted before REDACT_START, everything is by REDACT_END.
  var REDACT_START = 0.05;
  var REDACT_END = 0.94;
  var opacity = FIELD_MIN;
  var progress = 0;

  // Fictional samples, shaped like the data Husonym actually masks.
  var SAMPLES = [
    'marie.dupont@acme.fr', '+33 6 12 34 56 78', '1 84 07 75 116 300',
    'FR76 3000 6000 0112', '4539 1488 0343 6467', 'Camille Fontaine',
    '12 rue des Lilas', 'user_8842', '1987-04-23', 'SIRET 552 100 554',
    'j.okafor@example.com', '+1 415 555 0132', 'passport X4K88213',
  ];

  // Colours are read back out of the CSS custom properties, so no hex is hardcoded
  // here and the brand sheet stays the single source of truth (see base.css).
  var css = getComputedStyle(document.documentElement);
  var piiRgb = toRgb(css.getPropertyValue('--data-pii'), [248, 113, 113]);
  var maskedRgb = toRgb(css.getPropertyValue('--data-masked'), [56, 189, 248]);
  var mono = (css.getPropertyValue('--font-mono') || 'monospace').trim();

  function toRgb(value, fallback) {
    var hex = (value || '').trim();
    var m = /^#([0-9a-f]{6})$/i.exec(hex);
    if (!m) return fallback;
    var n = parseInt(m[1], 16);
    return [(n >> 16) & 255, (n >> 8) & 255, n & 255];
  }

  function rgba(c, a) {
    return 'rgba(' + c[0] + ',' + c[1] + ',' + c[2] + ',' + a.toFixed(3) + ')';
  }

  var root = document.documentElement;
  var probe = document.querySelector('.mask-probe');
  var probeVal = probe && probe.querySelector('.val');
  // The readout label is authored in the page (data-mask-label), so this script never
  // needs the content catalogue — same trick as the contact form status messages.
  var probeLabel = (probe && probe.getAttribute('data-mask-label')) || 'PII masked';
  // Track of the readout, in % of the viewport height: it never leaves the viewport.
  var PROBE_TOP = 6;
  var PROBE_TRAVEL = 88;
  var scrollTimer = null;

  function markScrolling() {
    root.classList.add('is-scrolling');
    if (scrollTimer) clearTimeout(scrollTimer);
    scrollTimer = setTimeout(function () { root.classList.remove('is-scrolling'); }, 900);
  }

  var dpr = Math.min(window.devicePixelRatio || 1, 2);
  var bits = [];

  function sizeCanvas() {
    dpr = Math.min(window.devicePixelRatio || 1, 2);
    canvas.width = window.innerWidth * dpr;
    canvas.height = window.innerHeight * dpr;
    ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
    ctx.font = '11px ' + mono;
    ctx.textBaseline = 'middle';
  }

  function seedBits() {
    // Density proportional to the visible area, bounded to stay cheap: every fragment
    // costs a fillText per frame.
    var count = Math.round((window.innerWidth * window.innerHeight) / 26000);
    count = Math.max(24, Math.min(80, count));
    bits = [];
    for (var i = 0; i < count; i++) bits.push(makeBit(Math.random() * window.innerHeight));
  }

  function makeBit(y) {
    var text = SAMPLES[Math.floor(Math.random() * SAMPLES.length)];
    var w = ctx.measureText(text).width;
    // Own redaction window: the field turns over gradually rather than all at once,
    // and each fragment takes a stretch of scroll to go from clear to fully covered.
    var span = 0.2 + Math.random() * 0.25;
    var from = REDACT_START + Math.random() * (REDACT_END - REDACT_START - span);
    return {
      x: Math.random() * window.innerWidth,
      y: y,
      text: text,
      w: w,
      cw: w / text.length, // the font is monospace: one cell per character
      vy: 0.12 + Math.random() * 0.38,
      drift: (Math.random() - 0.5) * 0.2,
      a: 0.35 + Math.random() * 0.65,
      from: from,
      to: from + span,
      order: redactionOrder(text),
      covered: -1, // count the bars were last computed for
      bars: [],
    };
  }

  // The order in which the characters of a fragment get covered: one segment (a run
  // between separators) after another, left to right inside a segment, the segments
  // themselves shuffled. Separators go last, which is what fuses the bars into one.
  // Drawn once per fragment, so the bars only ever grow as the scroll advances.
  function redactionOrder(text) {
    var segments = [];
    var separators = [];
    var current = null;
    for (var i = 0; i < text.length; i++) {
      if (/[\s.@_+-]/.test(text.charAt(i))) {
        separators.push(i);
        current = null;
      } else {
        if (!current) { current = []; segments.push(current); }
        current.push(i);
      }
    }
    for (var j = segments.length - 1; j > 0; j--) {
      var k = Math.floor(Math.random() * (j + 1));
      var swap = segments[j]; segments[j] = segments[k]; segments[k] = swap;
    }
    return [].concat.apply([], segments).concat(separators);
  }

  // The bars covering the first `count` characters of the order, as [first cell, cells]
  // pairs with adjacent cells merged so that a bar has no seam.
  function redactionBars(b, count) {
    var hidden = [];
    for (var i = 0; i < count; i++) hidden[b.order[i]] = true;
    var bars = [];
    var start = -1;
    for (var c = 0; c <= b.text.length; c++) {
      if (hidden[c]) {
        if (start < 0) start = c;
      } else if (start >= 0) {
        bars.push([start, c - start]);
        start = -1;
      }
    }
    return bars;
  }

  function draw() {
    ctx.clearRect(0, 0, window.innerWidth, window.innerHeight);
    var coveredChars = 0;
    var totalChars = 0;
    for (var i = 0; i < bits.length; i++) {
      var b = bits[i];
      var len = b.text.length;
      var ratio = Math.min(1, Math.max(0, (progress - b.from) / (b.to - b.from)));
      var count = Math.round(ratio * len);
      if (count !== b.covered) {
        b.covered = count;
        b.bars = redactionBars(b, count);
      }
      coveredChars += count;
      totalChars += len;
      if (count < len) {
        // Not scaled by the field opacity, unlike the bars: the canvas already is, and
        // the data in the clear has to stay readable at the top of the page.
        ctx.fillStyle = rgba(piiRgb, b.a * TEXT_ALPHA);
        ctx.fillText(b.text, b.x, b.y);
      }
      ctx.fillStyle = rgba(maskedRgb, b.a * opacity * 0.55);
      for (var j = 0; j < b.bars.length; j++) {
        var x = b.x + b.bars[j][0] * b.cw;
        var w = b.bars[j][1] * b.cw;
        // The bar is translucent: wipe the glyphs under it first, or they show through.
        ctx.clearRect(x, b.y - 7, w, 14);
        ctx.fillRect(x, b.y - 5, w, 10);
      }
    }
    updateProbe(coveredChars / (totalChars || 1));
  }

  function updateProbe(ratio) {
    if (!probe) return;
    probe.style.top = (PROBE_TOP + progress * PROBE_TRAVEL) + '%';
    probeVal.textContent = probeLabel + ' · ' + Math.round(ratio * 100) + ' %';
  }

  // The readout doubles as a scroll handle: dragging it moves the page along the very
  // track updateProbe() places it on, so it stays under the pointer.
  function armProbeDrag() {
    if (!probe) return;
    var grab = 0; // pointer offset from the centre of the readout, kept during the drag

    function onMove(e) {
      var at = ((e.clientY - grab) / window.innerHeight * 100 - PROBE_TOP) / PROBE_TRAVEL;
      // 'instant': html{scroll-behavior:smooth} would otherwise lag behind the pointer.
      window.scrollTo({ top: Math.min(1, Math.max(0, at)) * scrollMax(), behavior: 'instant' });
    }

    function onRelease(e) {
      probe.releasePointerCapture(e.pointerId);
      probe.removeEventListener('pointermove', onMove);
      root.classList.remove('is-probe-dragging');
    }

    probe.addEventListener('pointerdown', function (e) {
      var box = probe.getBoundingClientRect();
      grab = e.clientY - (box.top + box.height / 2);
      probe.setPointerCapture(e.pointerId);
      probe.addEventListener('pointermove', onMove);
      root.classList.add('is-probe-dragging');
      e.preventDefault(); // no text selection while dragging
    });
    probe.addEventListener('pointerup', onRelease);
    probe.addEventListener('pointercancel', onRelease);
  }

  function step() {
    for (var i = 0; i < bits.length; i++) {
      var b = bits[i];
      b.y += b.vy;
      b.x += b.drift;
      if (b.y - 6 > window.innerHeight) {
        // Recycled with a fresh sample and threshold, so the field never loops visibly.
        bits[i] = makeBit(-6);
      } else if (b.x < -b.w - 5) {
        b.x = window.innerWidth + 5;
      } else if (b.x > window.innerWidth + 5) {
        b.x = -b.w - 5;
      }
    }
  }

  function loop() { step(); draw(); requestAnimationFrame(loop); }

  // body{overflow-x:hidden} makes <body> the scrolling element rather than <html>,
  // so read scrollingElement — documentElement would report no progress at all.
  function scrollingElement() {
    return document.scrollingElement || document.documentElement;
  }

  function scrollMax() {
    return scrollingElement().scrollHeight - window.innerHeight;
  }

  function scrollProgress() {
    var max = scrollMax();
    var top = window.scrollY || scrollingElement().scrollTop || 0;
    return max > 0 ? Math.min(1, Math.max(0, top / max)) : 0;
  }

  function onScroll() {
    progress = scrollProgress();
    opacity = FIELD_MIN + Math.pow(progress, 1.4) * (FIELD_MAX - FIELD_MIN);
    canvas.style.opacity = String(opacity);
    markScrolling();
    if (reduce) draw(); // no animation loop in reduced motion: repaint on scroll only
  }

  sizeCanvas();
  seedBits();
  onScroll();
  armProbeDrag();
  window.addEventListener('scroll', onScroll, { passive: true });
  window.addEventListener('resize', function () { sizeCanvas(); seedBits(); if (reduce) draw(); });

  if (!reduce) loop();
})();
