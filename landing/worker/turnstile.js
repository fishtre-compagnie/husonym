// Cloudflare Turnstile verification.
//
// Convention shared with the build: an unset value means "not configured". No secret
// here and no site key at build time means no widget and no verification, so the form
// stays usable locally and on previews. The two are set together, or not at all.
const VERIFY_URL = 'https://challenges.cloudflare.com/turnstile/v0/siteverify';
const TIMEOUT_MS = 5000;

export async function verifyTurnstile(env, token, ip) {
  const secret = env.TURNSTILE_SECRET_KEY;
  if (!secret) return { ok: true };

  if (typeof token !== 'string' || !token) {
    // Secret set but no token: the site key was forgotten at build time. That is a
    // configuration error, and it belongs in the logs rather than in guesswork.
    console.warn('contact: Turnstile token missing while verification is enabled');
    return { ok: false, status: 403, code: 'captcha_required' };
  }

  try {
    const response = await fetch(VERIFY_URL, {
      method: 'POST',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify({ secret, response: token, remoteip: ip }),
      signal: AbortSignal.timeout(TIMEOUT_MS),
    });
    const verdict = await response.json();
    if (!verdict.success) {
      console.warn('contact: Turnstile rejected', verdict['error-codes']);
      return { ok: false, status: 403, code: 'captcha_failed' };
    }
    return { ok: true };
  } catch (err) {
    // Fail CLOSED. Letting requests through when the verifier is unreachable would
    // turn the form into exactly the door Turnstile is there to shut.
    console.error('contact: Turnstile verification unreachable', err);
    return { ok: false, status: 503, code: 'captcha_unavailable' };
  }
}
