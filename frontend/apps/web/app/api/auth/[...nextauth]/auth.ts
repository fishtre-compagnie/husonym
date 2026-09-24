import { type TokenSet } from '@auth/core/types';
import { addSeconds, isAfter } from 'date-fns';
import NextAuth, { NextAuthConfig } from 'next-auth';
import { NextRequest } from 'next/server';
import {
  AccountLoginMethod,
  getProviderId,
  fetchAccountLoginMethod,
} from './account-provider';
import { FlowAccount, getRequestAccount } from './login-flow';

function getProviders(
  accountMethod: AccountLoginMethod | null
): NextAuthConfig['providers'] {
  const providers: NextAuthConfig['providers'] = [];
  const authConfig = getOAuthConfig(accountMethod);
  if (authConfig) {
    providers.push({
      id: authConfig.id,
      name: authConfig.name,
      type: authConfig.type,
      issuer: authConfig.expectedissuer,
      clientId: authConfig.clientId,
      clientSecret: authConfig.clientSecret ?? '',
      authorization: {
        url: authConfig.authorizeUrl,
        params: {
          audience: authConfig.audience,
          scope: authConfig.scope,
        },
      },
      userinfo: authConfig.userInfoUrl,
      token: authConfig.tokenUrl,

      wellKnown: getWellKnown(authConfig.issuer),
      // An account's provider is reached as a public client: without a secret, Auth.js
      // would otherwise authenticate with an empty one, which providers refuse.
      // A public client has no secret: PKCE is what binds the code to this flow, and it
      // is required, not merely accepted.
      ...(authConfig.clientSecret
        ? {}
        : {
            client: { token_endpoint_auth_method: 'none' },
            checks: ['pkce', 'state'],
          }),
    });
  }

  return providers;
}

function getWellKnown(issuerUrl: string): string {
  return `${trimEnd(issuerUrl, '/')}/.well-known/openid-configuration`;
}

function trimEnd(val: string, chars: string): string {
  return val.endsWith(chars)
    ? val.substring(0, val.length - chars.length)
    : val;
}

interface OAuthConfig {
  id: string;
  name: string;
  type: 'oidc';

  issuer: string;
  expectedissuer: string;

  authorizeUrl?: string;
  userInfoUrl?: string;
  tokenUrl?: string;
  logoutUrl?: string;

  clientId: string;
  clientSecret?: string;
  audience: string;
  scope: string;
  // The provider is an account's, not the deployment's.
  account?: boolean;
}

/**
 * Where to end the provider's session: that of the provider the session came from. An
 * account's provider is only ever discovered; the deployment's may be configured.
 */
export async function getLogoutUrl(
  accountIssuer: string | undefined
): Promise<string | undefined> {
  const oauthconfig = accountIssuer
    ? { issuer: accountIssuer, logoutUrl: undefined }
    : getOAuthConfig(null);
  if (!oauthconfig) {
    console.warn('there is no oauthconfig defined, unable to find logout url');
    return undefined;
  }
  if (oauthconfig.logoutUrl) {
    return oauthconfig.logoutUrl;
  }
  const oidcConfig = await getOpenIdConfiguration(
    oauthconfig.issuer,
    !!accountIssuer
  );
  const endSession = oidcConfig.end_session_endpoint;
  if (endSession) {
    // An account's provider sends its users back to its own origin, nowhere else.
    if (
      accountIssuer &&
      new URL(endSession).origin !== new URL(accountIssuer).origin
    ) {
      console.warn(
        'the end session endpoint of the account provider is not on its origin'
      );
      return undefined;
    }
    return endSession;
  }
  console.warn(
    'oidc configuration well known did not provide an end session endpoint'
  );
  return undefined;
}

/**
 * Builds the provider, from the account's declaration when there is one and from the
 * deployment's variables otherwise.
 *
 * An account that declares a provider replaces the issuer and the client, and nothing
 * else: the id stays fixed (one redirect URI for the deployment -- see account-provider),
 * the scope and audience stay the deployment's, and the endpoints stay discovered rather
 * than configured, because an account has no way to override them and should not.
 */
function getOAuthConfig(
  accountMethod: AccountLoginMethod | null
): OAuthConfig | null {
  const issuer = accountMethod?.issuer ?? process.env.AUTH_ISSUER;
  const clientId = accountMethod?.clientId ?? process.env.AUTH_CLIENT_ID;
  // Never the deployment's secret against an account's provider: that would send this
  // deployment's client secret to a server the account chose.
  const clientSecret = accountMethod
    ? undefined
    : process.env.AUTH_CLIENT_SECRET;
  const audience = process.env.AUTH_AUDIENCE;
  const scope = process.env.AUTH_SCOPE;
  if (!issuer || !clientId || !audience || !scope) {
    return null;
  }

  const id = getProviderId();
  const name = process.env.AUTH_PROVIDER_NAME ?? 'unknown';
  const expectedissuer = accountMethod
    ? issuer
    : process.env.AUTH_EXPECTED_ISSUER || issuer;

  return {
    id,
    name,
    type: 'oidc',

    issuer,
    expectedissuer,
    clientId,
    clientSecret,
    audience,
    scope,

    // Endpoint overrides are a deployment's business. An account declares an issuer and
    // lets discovery do the rest, which is what "any compliant provider" means.
    authorizeUrl: accountMethod ? undefined : process.env.AUTH_AUTHORIZE_URL,
    userInfoUrl: accountMethod ? undefined : process.env.AUTH_USERINFO_URL,
    logoutUrl: accountMethod ? undefined : process.env.AUTH_LOGOUT_URL,
    tokenUrl: accountMethod ? undefined : process.env.AUTH_TOKEN_URL,
  };
}

/**
 * The configuration is a function of the request, which is what Auth.js v5 accepts and
 * what makes a provider per account possible without forking anything.
 *
 * `request` is undefined outside a request -- `auth()` in a server component, middleware
 * -- and that has to yield a valid configuration rather than throw: the absence of an
 * account is the deployment's own provider, which is the normal case.
 */
export const {
  handlers: { GET, POST },
  // auth function meant to be used in RSC or middleware.
  auth,
} = NextAuth(async (request: NextRequest | undefined) => {
  return buildConfig(await getRequestAccount(request));
});

function buildConfig(accountMethod: FlowAccount | null): NextAuthConfig {
  return {
    providers: getProviders(accountMethod),
    session: { strategy: 'jwt' },
    callbacks: {
      session: async ({ session, token }) => {
        session.accessToken = (token as any).accessToken; // eslint-disable-line @typescript-eslint/no-explicit-any
        session.idToken = (token as any).idToken; // eslint-disable-line @typescript-eslint/no-explicit-any
        session.accountIssuer = (token as any).accountIssuer; // eslint-disable-line @typescript-eslint/no-explicit-any
        return session;
      },
      jwt: async ({ token, account }) => {
        // Persist the OAuth access_token and or the user id to the token right after signin
        if (account) {
          token.idToken = account.id_token;
          token.accessToken = account.access_token;
          token.refreshToken = account.refresh_token;
          token.expiresAt = account.expires_at;
          token.provider = account.provider;
          // The provider that issued these tokens, when it is an account's: refreshing them
          // and ending the session go to it, whatever account a later request names. Held
          // in the session token, which only this server can read or write.
          token.accountIssuer = accountMethod?.issuer;
          token.accountClientId = accountMethod?.clientId;
          token.accountSlug = accountMethod?.slug;
        }
        if (
          !token.expiresAt ||
          // Both times must be in the same format
          isAfter(new Date(), new Date((token as any).expiresAt * 1000)) // eslint-disable-line @typescript-eslint/no-explicit-any
        ) {
          // refresh token
          if (!token.refreshToken) {
            // token can't be refreshed, fail
            throw new Error('session is expired, no refresh token available');
          }

          const oauthConfig = await getRefreshConfig(token);
          if (!oauthConfig) {
            throw new Error('unable to find provider to refresh token');
          }
          try {
            const response = await fetch(
              oauthConfig.tokenUrl ??
                (await getTokenUrl(oauthConfig.issuer, !!oauthConfig.account)),
              {
                headers: {
                  'Content-Type': 'application/x-www-form-urlencoded',
                },
                body: new URLSearchParams({
                  client_id: oauthConfig.clientId,
                  // A public client sends no secret; the deployment's secret never goes
                  // to an account's provider.
                  ...(oauthConfig.clientSecret
                    ? { client_secret: oauthConfig.clientSecret }
                    : {}),
                  grant_type: 'refresh_token',
                  refresh_token: (token as any).refreshToken, // eslint-disable-line @typescript-eslint/no-explicit-any
                }),
                method: 'POST',
              }
            );
            const tokens: TokenSet = await response.json();
            if (!response.ok) {
              throw tokens;
            }
            token.accessToken = tokens.access_token;
            // the refresh token may not always be returned. If it's not, don't update
            if (tokens.refresh_token) {
              token.refreshToken = tokens.refresh_token;
            }
            if (tokens.expires_at) {
              token.expiresAt = tokens.expires_at;
            } else if (tokens.expires_in) {
              token.expiresAt = Math.floor(
                addSeconds(new Date(), tokens.expires_in).getTime() / 1000
              );
            }
          } catch (err) {
            console.error('failed to refresh token', err);
            throw err;
          }
        }
        return token;
      },
    },
  };
}

/**
 * The provider a session's tokens are refreshed with: the account's that issued them, or
 * the deployment's for a session that came from it.
 */
//
// An account's provider is asked again each time: an account that has since changed or
// withdrawn it — after a compromise, say — ends the sessions it issued, rather than let
// them go on refreshing, and sending their refresh tokens, to a provider it no longer
// trusts.
async function getRefreshConfig(
  token: Record<string, unknown>
): Promise<Pick<
  OAuthConfig,
  'issuer' | 'clientId' | 'clientSecret' | 'tokenUrl' | 'account'
> | null> {
  const { accountIssuer, accountClientId, accountSlug } = token;
  if (typeof accountIssuer !== 'string') {
    return getOAuthConfig(null);
  }
  const current =
    typeof accountSlug === 'string'
      ? await fetchAccountLoginMethod(accountSlug)
      : null;
  if (
    current?.issuer !== accountIssuer ||
    current.clientId !== accountClientId
  ) {
    return null;
  }
  return { issuer: accountIssuer, clientId: current.clientId, account: true };
}

interface OidcConfiguration {
  issuer: string;
  authorization_endpoint: string;
  token_endpoint: string;
  userinfo_endpoint: string;
  end_session_endpoint?: string;
  jwks_uri: string;
}

async function getTokenUrl(
  issuer: string,
  isAccountIssuer: boolean
): Promise<string> {
  try {
    const oidcConfig = await getOpenIdConfiguration(issuer, isAccountIssuer);
    if (!oidcConfig.token_endpoint) {
      throw new Error('unable to find token endpoint');
    }
    return oidcConfig.token_endpoint;
  } catch (err) {
    throw err;
  }
}

/**
 * The discovery document of a provider. An account's must call itself by the issuer the
 * account declared, as Auth.js requires when signing in: otherwise its endpoints are not
 * that provider's. The deployment's is exempt, since its issuer may be an internal URL
 * the document names differently (AUTH_EXPECTED_ISSUER).
 */
async function getOpenIdConfiguration(
  issuer: string,
  isAccountIssuer: boolean
): Promise<Partial<OidcConfiguration>> {
  const wellKnownUrl = getWellKnown(issuer);
  const res = await fetch(wellKnownUrl, {
    method: 'GET',
    headers: { 'Content-Type': 'application/json' },
    signal: AbortSignal.timeout(DISCOVERY_TIMEOUT_MS),
  });
  const doc = (await res.json()) as Partial<OidcConfiguration>;
  if (isAccountIssuer && doc.issuer !== issuer) {
    throw new Error(
      'the discovery document of the account provider names another issuer'
    );
  }
  return doc;
}

// How long a provider's discovery document may take to answer.
const DISCOVERY_TIMEOUT_MS = 10_000;

declare module 'next-auth' {
  export interface Session {
    accessToken: string;
    idToken: string;
    // The issuer of the account's provider the session came from; unset for the
    // deployment's.
    accountIssuer?: string;
  }
}

// This isn't currently working, guessing because next-auth has its own local dependency of @auth/core due to mismatched versions
// declare module '@auth/core/jwt' {
//   export interface JWT {
//     refreshToken?: string;
//     expiresAt?: number;
//     accessToken?: string;
//   }
// }
