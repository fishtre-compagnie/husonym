// Mobile menu: the burger opens and closes the drop-down panel (.nav.open). Without
// JS the panel stays folded — the same links remain reachable from the footer.
(function () {
  var nav = document.querySelector('.nav');
  var toggle = nav && nav.querySelector('.nav-toggle');
  if (!nav || !toggle) return;

  function setOpen(open) {
    nav.classList.toggle('open', open);
    toggle.setAttribute('aria-expanded', String(open));
  }

  toggle.addEventListener('click', function () {
    setOpen(!nav.classList.contains('open'));
  });

  // Clicking a menu link closes the panel (anchor navigation).
  nav.querySelectorAll('.nav-collapse a').forEach(function (a) {
    a.addEventListener('click', function () { setOpen(false); });
  });

  document.addEventListener('keydown', function (e) {
    if (e.key === 'Escape') setOpen(false);
  });

  // Fold back when crossing above the breakpoint (the panel becomes the bar again).
  // Kept in sync with the max-width in nav.css.
  var mq = window.matchMedia('(max-width:1100px)');
  mq.addEventListener('change', function (e) { if (!e.matches) setOpen(false); });
})();
