import { NextRequest } from 'next/server';

/**
 * Which account a sign-in is for, and the provider it uses.
 *
 * The tenant has to be known before the flow starts: an OIDC flow begins with the client
 * id of the right connector, and at that point nobody has proved anything. The account is
 * therefore designated by the link that was followed -- the deterministic answer, and the
 * only one that asks nobody to prove ownership of an email domain.
 */

/**
 * The cookie that carries the account across the two legs of the flow.
 *
 * The callback leg arrives with only `code` and `state`, and the configuration has to
 * resolve the same account then as it did at the start -- otherwise it presents the wrong
 * client to the token endpoint. The query parameter is only present on the first leg, so
 * something has to remember it in between; a cookie is what the browser carries anyway.
 *
 * It holds a slug, which is public, never anything about a person.
 */
export const ACCOUNT_COOKIE = 'husonym.login-account';

export interface AccountLoginMethod {
  issuer: string;
  clientId: string;
}

/**
 * The provider id is deliberately FIXED, and this is the decision the rest depends on.
 *
 * Auth.js routes callbacks to `/api/auth/callback/<providerId>`, so an id that varied by
 * account would mean a different redirect URI per account -- and every account having to
 * register its own with its provider. One id, with the issuer and client varying behind
 * it, means one redirect URI for the whole deployment: the thing an operator registers
 * once and never touches again.
 */
export const PROVIDER_ID = 'oidc';

export function getAccountSlug(
  request: NextRequest | undefined
): string | null {
  if (!request) {
    return null;
  }
  const fromQuery = request.nextUrl.searchParams.get('account');
  if (fromQuery) {
    return sanitizeSlug(fromQuery);
  }
  const fromCookie = request.cookies.get(ACCOUNT_COOKIE)?.value;
  return fromCookie ? sanitizeSlug(fromCookie) : null;
}

/**
 * A slug reaches this from a URL or a cookie, and is about to be put in a request to the
 * backend. Constraining its shape here means nothing further down has to wonder.
 */
export function sanitizeSlug(value: string): string | null {
  const trimmed = value.trim();
  if (!/^[a-zA-Z0-9][a-zA-Z0-9_-]{0,99}$/.test(trimmed)) {
    return null;
  }
  return trimmed;
}

/**
 * Asks the backend which provider an account signs in with.
 *
 * Returns null for an account that has declared none, which is also the answer for a slug
 * the backend does not recognize — the caller then falls back on the deployment's own
 * provider. The two cases are deliberately indistinguishable: this runs before anybody has
 * authenticated, so it must not become a way to find out what exists.
 */
export async function fetchAccountLoginMethod(
  slug: string
): Promise<AccountLoginMethod | null> {
  const baseUrl = process.env.HUSONYM_API_BASE_URL;
  if (!baseUrl) {
    return null;
  }

  try {
    const res = await fetch(
      `${trimEnd(baseUrl, '/')}/mgmt.v1alpha1.AuthService/GetAccountLoginMethod`,
      {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ accountSlug: slug }),
        cache: 'no-store',
      }
    );
    if (!res.ok) {
      return null;
    }
    const body = (await res.json()) as Partial<AccountLoginMethod>;
    if (!body.issuer || !body.clientId) {
      return null;
    }
    return { issuer: body.issuer, clientId: body.clientId };
  } catch (err) {
    // A sign-in must not fail because this lookup did: falling back on the deployment's
    // provider is what an account with no setting does anyway.
    console.warn('unable to resolve the account login method', err);
    return null;
  }
}

function trimEnd(val: string, chars: string): string {
  return val.endsWith(chars)
    ? val.substring(0, val.length - chars.length)
    : val;
}
