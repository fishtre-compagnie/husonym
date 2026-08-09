// Contact form → POST /api/contact, served by the same Worker that serves this page
// (see worker/contact.js). Same-origin, so there is no API origin to inject at build
// time and no CORS to configure — a preview deployment posts to its own preview.
(() => {
  const API_URL = '/api/contact';
  const form = document.querySelector('[data-contact-form]');
  if (!form) return;

  const submitBtn = form.querySelector('[data-contact-submit]');
  const status = form.querySelector('[data-contact-status]');
  const msg = (name) => form.querySelector(`[data-msg-${name}]`)?.textContent || '';

  const setStatus = (text, kind) => {
    status.textContent = text;
    status.classList.toggle('is-success', kind === 'success');
    status.classList.toggle('is-error', kind === 'error');
  };

  form.addEventListener('submit', async (event) => {
    event.preventDefault();
    if (!form.reportValidity()) return;

    const data = Object.fromEntries(new FormData(form).entries());
    // Lets the notification say which language and which page the lead came from.
    data.locale = document.documentElement.lang;
    data.page = location.pathname;

    submitBtn.disabled = true;
    setStatus(msg('sending'));
    try {
      const response = await fetch(API_URL, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(data),
      });
      if (response.ok) {
        form.reset();
        setStatus(msg('success'), 'success');
      } else if (response.status === 400) {
        setStatus(msg('invalid'), 'error');
      } else if (response.status === 403) {
        setStatus(msg('captcha'), 'error');
      } else if (response.status === 429) {
        setStatus(msg('limit'), 'error');
      } else {
        setStatus(msg('error'), 'error');
      }
    } catch {
      setStatus(msg('error'), 'error');
    } finally {
      // A Turnstile token is SINGLE USE: without this reset a second send (after a
      // success as much as after an error) would reuse a spent token and be refused.
      // `turnstile` is absent when no site key was injected at build time.
      window.turnstile?.reset();
      // In `finally` rather than per branch: forgotten on the success path, it left the
      // button greyed out for good and a visitor wanting to send a second message had
      // to reload the page.
      submitBtn.disabled = false;
    }
  });
})();
