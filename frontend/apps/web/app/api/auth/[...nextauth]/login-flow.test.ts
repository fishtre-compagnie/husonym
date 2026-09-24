import { NextRequest } from 'next/server';
import {
  FLOW_COOKIE,
  FlowAccount,
  getRequestAccount,
  openFlow,
  sealFlow,
} from './login-flow';

const acme: FlowAccount = {
  slug: 'acme',
  issuer: 'https://idp.acme.example/realms/acme',
  clientId: 'husonym',
};

describe('the sealed flow', () => {
  const secret = process.env.NEXTAUTH_SECRET;
  beforeEach(() => {
    process.env.NEXTAUTH_SECRET = 'test-secret';
  });
  afterEach(() => {
    process.env.NEXTAUTH_SECRET = secret;
  });

  it('opens to the account it was sealed for', async () => {
    expect(await openFlow(await sealFlow(acme))).toEqual(acme);
  });

  it('does not open once altered', async () => {
    const [body, signature] = (await sealFlow(acme)).split('.');
    const forged = Buffer.from(
      JSON.stringify({ ...acme, issuer: 'https://evil.example', exp: 9e9 })
    ).toString('base64url');
    expect(await openFlow(`${forged}.${signature}`)).toBeNull();
    expect(await openFlow(`${body}.${signature}x`)).toBeNull();
    expect(await openFlow(`${body}`)).toBeNull();
    expect(await openFlow(`${body}.${signature}.more`)).toBeNull();
    expect(await openFlow(undefined)).toBeNull();
  });

  it('does not open under another secret', async () => {
    const sealed = await sealFlow(acme);
    process.env.NEXTAUTH_SECRET = 'another-secret';
    expect(await openFlow(sealed)).toBeNull();
  });

  it('does not open once expired', async () => {
    const sealed = await sealFlow(acme, Date.now());
    expect(await openFlow(sealed, Date.now() + 16 * 60 * 1000)).toBeNull();
  });

  it('is what the callback uses, and nothing else is', async () => {
    const sealed = await sealFlow(acme);
    const callback = new NextRequest(
      'http://app.example/api/auth/callback/oidc?code=c&state=s',
      {
        headers: {
          cookie: `${FLOW_COOKIE}=${sealed}; husonym.login-account=evil`,
        },
      }
    );
    expect(await getRequestAccount(callback)).toEqual(acme);

    // Without the sealed flow, the callback is the deployment's, whatever account the
    // link cookie names.
    const unsealed = new NextRequest(
      'http://app.example/api/auth/callback/oidc?code=c&state=s',
      { headers: { cookie: 'husonym.login-account=evil' } }
    );
    expect(await getRequestAccount(unsealed)).toBeNull();

    // No other route of Auth.js is made with an account's provider.
    const session = new NextRequest('http://app.example/api/auth/session', {
      headers: {
        cookie: `${FLOW_COOKIE}=${sealed}; husonym.login-account=acme`,
      },
    });
    expect(await getRequestAccount(session)).toBeNull();
    expect(await getRequestAccount(undefined)).toBeNull();
  });
});
