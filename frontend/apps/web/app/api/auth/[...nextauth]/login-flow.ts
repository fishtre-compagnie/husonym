import { NextRequest } from 'next/server';
import {
  ACCOUNT_COOKIE,
  AccountLoginMethod,
  fetchAccountLoginMethod,
  sanitizeSlug,
} from './account-provider';

/**
 * The account a sign-in in progress is for, bound to that flow.
 *
 * The account a sign-in starts with is read from a cookie the per-account link sets, and
 * anyone can make a browser follow that link: rewritten between the start of a flow and
 * its callback, it made the callback exchange the code with another account's provider.
 * So the account is read once, when the sign-in starts — a POST that carries Auth.js's
 * CSRF token, which no other site can make the browser send — and written here, signed with
 * the deployment's secret, for the callback to use and nothing else.
 */
export const FLOW_COOKIE = 'husonym.login-flow';

// As long as the account cookie: a sign-in with a detour through a second factor.
export const FLOW_MAX_AGE_SECONDS = 15 * 60;

export interface FlowAccount extends AccountLoginMethod {
  slug: string;
}

interface FlowPayload extends FlowAccount {
  // When the flow expires, in seconds since the epoch.
  exp: number;
}

// Where Auth.js answers.
const AUTH_BASE_PATH = '/api/auth';

export function isSignInStart(req: NextRequest): boolean {
  return (
    req.method === 'POST' &&
    req.nextUrl.pathname.startsWith(`${AUTH_BASE_PATH}/signin/`)
  );
}

export function isCallback(req: NextRequest): boolean {
  return req.nextUrl.pathname.startsWith(`${AUTH_BASE_PATH}/callback/`);
}

/**
 * The account whose provider a request to Auth.js is made with: the one asked for, when a
 * sign-in starts; the one sealed at that start, on the callback; the deployment's (null)
 * everywhere else. A session is refreshed with the provider its token names (auth.ts),
 * whatever the request.
 */
export async function getRequestAccount(
  req: NextRequest | undefined
): Promise<FlowAccount | null> {
  if (!req) {
    return null;
  }
  if (isCallback(req)) {
    return openFlow(req.cookies.get(FLOW_COOKIE)?.value);
  }
  if (isSignInStart(req)) {
    return getAskedAccount(req);
  }
  return null;
}

// getAskedAccount is the account the per-account link asked a sign-in for, if any.
export async function getAskedAccount(
  req: NextRequest
): Promise<FlowAccount | null> {
  const slug = sanitizeSlug(req.cookies.get(ACCOUNT_COOKIE)?.value ?? '');
  if (!slug) {
    return null;
  }
  const method = await fetchAccountLoginMethod(slug);
  return method ? { ...method, slug } : null;
}

// What the signature is for, so that it cannot be taken for another use of the secret.
const PURPOSE = 'husonym.login-flow.v1';

export async function sealFlow(
  account: FlowAccount,
  now: number = Date.now()
): Promise<string> {
  const payload: FlowPayload = {
    ...account,
    exp: Math.floor(now / 1000) + FLOW_MAX_AGE_SECONDS,
  };
  const body = toBase64Url(new TextEncoder().encode(JSON.stringify(payload)));
  return `${body}.${await sign(body)}`;
}

/**
 * The account of the flow the cookie seals, or null when it is absent, altered, expired or
 * sealed under another secret.
 */
export async function openFlow(
  sealed: string | undefined,
  now: number = Date.now()
): Promise<FlowAccount | null> {
  if (!sealed) {
    return null;
  }
  const [body, signature, ...rest] = sealed.split('.');
  if (!body || !signature || rest.length > 0) {
    return null;
  }
  if (!(await verify(body, signature))) {
    return null;
  }
  try {
    const payload = JSON.parse(
      new TextDecoder().decode(fromBase64Url(body))
    ) as Partial<FlowPayload>;
    if (
      typeof payload.slug !== 'string' ||
      typeof payload.issuer !== 'string' ||
      typeof payload.clientId !== 'string' ||
      typeof payload.exp !== 'number' ||
      payload.exp < Math.floor(now / 1000)
    ) {
      return null;
    }
    return {
      slug: payload.slug,
      issuer: payload.issuer,
      clientId: payload.clientId,
    };
  } catch {
    return null;
  }
}

async function key(): Promise<CryptoKey> {
  const secret = process.env.AUTH_SECRET || process.env.NEXTAUTH_SECRET;
  if (!secret) {
    throw new Error('no secret to sign the sign-in flow with');
  }
  return crypto.subtle.importKey(
    'raw',
    new TextEncoder().encode(secret),
    { name: 'HMAC', hash: 'SHA-256' },
    false,
    ['sign', 'verify']
  );
}

async function sign(body: string): Promise<string> {
  const signature = await crypto.subtle.sign(
    'HMAC',
    await key(),
    new TextEncoder().encode(`${PURPOSE}.${body}`)
  );
  return toBase64Url(new Uint8Array(signature));
}

async function verify(body: string, signature: string): Promise<boolean> {
  try {
    return await crypto.subtle.verify(
      'HMAC',
      await key(),
      fromBase64Url(signature),
      new TextEncoder().encode(`${PURPOSE}.${body}`)
    );
  } catch {
    return false;
  }
}

function toBase64Url(bytes: Uint8Array): string {
  return Buffer.from(bytes).toString('base64url');
}

function fromBase64Url(value: string): Uint8Array<ArrayBuffer> {
  const bytes = Buffer.from(value, 'base64url');
  const out = new Uint8Array(new ArrayBuffer(bytes.length));
  out.set(bytes);
  return out;
}
