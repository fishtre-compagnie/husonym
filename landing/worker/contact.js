// POST /api/contact — the only dynamic endpoint on the site.
//
// The layered defence, and its order, is ported from the Kachalot backend: each stage
// is more expensive and more discriminating than the one before it, so the cheap
// checks shed load before anything reaches the network or the mailer.
//
//   0. method / content-type / size / Origin   → 405, 415, 413, 403
//   1. honeypot                                → fake 200
//   2. field validation                        → 400   (no quota, no network spent)
//   3. per-IP ceiling                          → 429   (before any outbound call)
//   4. Turnstile siteverify                    → 403   (the one network round-trip)
//   5. per-sender ceiling                      → 429   (verified humans only)
//   6. send                                    → 503 on failure, never a hopeful 200
import { declaredTooLarge, isJson, json, readJson } from './http.js';
import { sendContactMail } from './mail.js';
import { verifyTurnstile } from './turnstile.js';

const MAX = { name: 200, email: 320, company: 200, message: 5000 };
const MAX_BODY_BYTES = 16 * 1024;
// Deliberately loose: this weeds out values that are not addresses at all. Anything
// stricter rejects valid addresses, and the only real proof of an address is a reply.
const EMAIL_SHAPE = /^[^\s@]+@[^\s@]+\.[^\s@]+$/;

export async function handleContact(request, env) {
  if (request.method !== 'POST') {
    return json({ error: 'method_not_allowed' }, 405, { allow: 'POST' });
  }
  if (!isJson(request)) return json({ error: 'unsupported_media_type' }, 415);
  if (declaredTooLarge(request, MAX_BODY_BYTES)) return json({ error: 'payload_too_large' }, 413);

  // Cross-site POSTs from a browser carry a foreign Origin. A MISSING Origin must not
  // fail: plenty of legitimate non-browser clients send none, and this header was
  // never a real authentication check to begin with.
  const origin = request.headers.get('origin');
  if (origin && env.SITE_ORIGIN && origin !== env.SITE_ORIGIN) {
    return json({ error: 'forbidden_origin' }, 403);
  }

  const body = await readJson(request, MAX_BODY_BYTES);
  if (!body || typeof body !== 'object') return json({ error: 'invalid_contact_request' }, 400);

  // Honeypot. A 400 here would tell the bot it was spotted; a 200 costs it nothing to
  // believe and teaches it nothing.
  if (body.website) return json({ sent: true }, 200);

  const input = parseContactInput(body);
  if (!input) return json({ error: 'invalid_contact_request' }, 400);

  const ip = request.headers.get('cf-connecting-ip') || 'unknown';

  // Counters are per Cloudflare location, not global: this is a throughput ceiling,
  // not an exact quota. Do not rely on it as a guarantee.
  if (!(await underLimit(env, `ip:${ip}`))) return json({ error: 'too_many_requests' }, 429);

  const verdict = await verifyTurnstile(env, body['cf-turnstile-response'], ip);
  if (!verdict.ok) return json({ error: verdict.code }, verdict.status);

  // Only charged once a human has been verified. Charging it earlier would let a bot
  // spoofing an address burn through its rightful owner's quota.
  if (!(await underLimit(env, `email:${input.email}`))) {
    return json({ error: 'too_many_requests' }, 429);
  }

  try {
    await sendContactMail(env, input, {
      locale: pickLocale(body.locale),
      page: safePath(body.page),
      country: request.cf?.country,
      ip,
    });
  } catch (err) {
    // Sending IS the service. A visitor told "sent" when nothing left will not come
    // back, so the failure is surfaced rather than swallowed.
    console.error('contact: send failed', err?.code || '', err?.message || err);
    return json({ error: 'contact_unavailable' }, 503);
  }

  return json({ sent: true }, 200);
}

async function underLimit(env, key) {
  if (!env.CONTACT_RATE_LIMITER) return true; // binding absent in some local setups
  const { success } = await env.CONTACT_RATE_LIMITER.limit({ key });
  return success;
}

// One definition of "valid", used for every field.
function cleanField(value, max) {
  if (typeof value !== 'string') return null;
  const trimmed = value.trim();
  if (!trimmed || trimmed.length > max) return null;
  return trimmed;
}

function parseContactInput(body) {
  const name = cleanField(body.name, MAX.name);
  const email = cleanField(body.email, MAX.email);
  const message = cleanField(body.message, MAX.message);
  if (!name || !email || !message || !EMAIL_SHAPE.test(email)) return null;
  // Company is optional: absent and blank both mean "not given".
  return { name, email, message, company: cleanField(body.company, MAX.company) };
}

function pickLocale(value) {
  return value === 'en' || value === 'fr' ? value : 'unknown';
}

// Only ever echoed back into an email we send ourselves, but it still comes from the
// client: keep it to a short same-site path so nothing arbitrary lands in the body.
function safePath(value) {
  if (typeof value !== 'string' || !value.startsWith('/') || value.length > 200) return '/';
  return value;
}
