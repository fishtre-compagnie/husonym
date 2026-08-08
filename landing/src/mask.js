// The page anonymizes itself as you scroll.
//
// A field of drifting data fragments sits behind the content. Near the top they are
// legible personal data drawn in the PII colour; each one carries its own depth
// threshold, and once the scroll passes that threshold the fragment is redrawn as a
// solid block in the masked colour. By the bottom of the page the field is entirely
// masked, and the gutter readout reports the real proportion — it counts fragments,
// it does not fake a number.
//
// The background gradient (mask.css) does the rest, with no JS at all.
(function () {
  var canvas = document.querySelector('.mask-field');
  if (!canvas || !canvas.getContext) return;
  var ctx = canvas.getContext('2d');
  var reduce = window.matchMedia('(prefers-reduced-motion: reduce)').matches;

  // Density curve: nothing before FIELD_START (top of page), then a gentle rise to
  // the FIELD_MAX ceiling further down.
  var FIELD_START = 0.16;
  var FIELD_MAX = 0.5;
  var opacity = 0;
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
    return {
      x: Math.random() * window.innerWidth,
      y: y,
      text: text,
      w: ctx.measureText(text).width,
      vy: 0.12 + Math.random() * 0.38,
      drift: (Math.random() - 0.5) * 0.2,
      a: 0.35 + Math.random() * 0.65,
      // Own masking threshold: the field turns over gradually rather than all at once.
      threshold: 0.12 + Math.random() * 0.72,
    };
  }

  function draw() {
    ctx.clearRect(0, 0, window.innerWidth, window.innerHeight);
    // The count runs even when the field is still fully transparent: the readout is
    // revealed by any scroll, and would otherwise flash an empty pill near the top.
    var visible = opacity > 0.001;
    var maskedCount = 0;
    for (var i = 0; i < bits.length; i++) {
      var b = bits[i];
      if (progress >= b.threshold) {
        maskedCount++;
        if (visible) {
          ctx.fillStyle = rgba(maskedRgb, b.a * opacity * 0.55);
          ctx.fillRect(b.x, b.y - 5, b.w, 10);
        }
      } else if (visible) {
        ctx.fillStyle = rgba(piiRgb, b.a * opacity);
        ctx.fillText(b.text, b.x, b.y);
      }
    }
    updateProbe(maskedCount / (bits.length || 1));
  }

  function updateProbe(ratio) {
    if (!probe) return;
    probe.style.top = (6 + progress * 88) + '%'; // keeps the label inside the viewport
    probeVal.textContent = probeLabel + ' · ' + Math.round(ratio * 100) + ' %';
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

  function scrollProgress() {
    // body{overflow-x:hidden} makes <body> the scrolling element rather than <html>,
    // so read scrollingElement — documentElement would report no progress at all.
    var se = document.scrollingElement || document.documentElement;
    var max = se.scrollHeight - window.innerHeight;
    var top = window.scrollY || se.scrollTop || 0;
    return max > 0 ? Math.min(1, Math.max(0, top / max)) : 0;
  }

  function onScroll() {
    progress = scrollProgress();
    var t = Math.max(0, (progress - FIELD_START) / (1 - FIELD_START));
    opacity = Math.pow(t, 1.4) * FIELD_MAX;
    canvas.style.opacity = String(opacity);
    markScrolling();
    if (reduce) draw(); // no animation loop in reduced motion: repaint on scroll only
  }

  sizeCanvas();
  seedBits();
  onScroll();
  window.addEventListener('scroll', onScroll, { passive: true });
  window.addEventListener('resize', function () { sizeCanvas(); seedBits(); if (reduce) draw(); });

  if (!reduce) loop();
})();
