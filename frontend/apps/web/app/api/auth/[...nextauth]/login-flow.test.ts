import { NextRequest } from 'next/server';
import {
  FLOW_HEADER,
  FlowAccount,
  getRequestAccount,
  sealForFlow,
  sealForHeader,
} from './login-flow';

const acme: FlowAccount = {
  slug: 'acme',
  issuer: 'https://idp.acme.example/realms/acme',
  clientId: 'husonym',
};

const secrets = ['AUTH_SECRET', 'NEXTAUTH_SECRET', 'AUTH_SECRET_1'];
const saved = Object.fromEntries(secrets.map((k) => [k, process.env[k]]));
beforeEach(() => {
  secrets.forEach((k) => delete process.env[k]);
  process.env.AUTH_SECRET = 'test-secret';
});
afterAll(() => {
  secrets.forEach((k) => {
    if (saved[k] === undefined) {
      delete process.env[k];
    } else {
      process.env[k] = saved[k];
    }
  });
});

function callback(state: string, cookie: string): NextRequest {
  return new NextRequest(
    `http://app.example/api/auth/callback/oidc?code=c&state=${state}`,
    { headers: { cookie } }
  );
}

describe('the account of a callback', () => {
  it('is the one sealed for the flow the callback ends', async () => {
    const sealed = await sealForFlow(acme, 'state-1');
    expect(
      await getRequestAccount(
        callback('state-1', `husonym.login-flow=${sealed}`)
      )
    ).toEqual(acme);
  });

  it('is not one sealed for another flow', async () => {
    // Someone who could set a cookie in the browser sets a seal they got for their own
    // flow: it names another state than the victim's.
    const sealed = await sealForFlow(acme, 'attacker-state');
    expect(
      await getRequestAccount(
        callback('victim-state', `husonym.login-flow=${sealed}`)
      )
    ).toBeNull();
  });

  it('is never read from the account cookie', async () => {
    expect(
      await getRequestAccount(callback('state-1', 'husonym.login-account=acme'))
    ).toBeNull();
  });

  it('is not one sealed for the configuration', async () => {
    const sealed = await sealForHeader(acme);
    expect(
      await getRequestAccount(
        callback('state-1', `husonym.login-flow=${sealed}`)
      )
    ).toBeNull();
  });

  it('is not one altered, expired, or sealed under another secret', async () => {
    const sealed = await sealForFlow(acme, 'state-1');
    const [body, signature] = sealed.split('.');
    const forged = btoa(
      JSON.stringify({ ...acme, issuer: 'https://evil.example', exp: 9e9 })
    );
    for (const value of [
      `${forged}.${signature}`,
      `${body}.${signature}x`,
      body,
      `${sealed}.more`,
    ]) {
      expect(
        await getRequestAccount(
          callback('state-1', `husonym.login-flow=${value}`)
        )
      ).toBeNull();
    }
    const old = await sealForFlow(acme, 'state-1', Date.now() - 16 * 60 * 1000);
    expect(
      await getRequestAccount(callback('state-1', `husonym.login-flow=${old}`))
    ).toBeNull();
    process.env.AUTH_SECRET = 'another-secret';
    expect(
      await getRequestAccount(
        callback('state-1', `husonym.login-flow=${sealed}`)
      )
    ).toBeNull();
  });
});

describe('the account of other requests', () => {
  it('is the one the route sealed when a sign-in starts', async () => {
    const start = new NextRequest('http://app.example/api/auth/signin/oidc', {
      method: 'POST',
      headers: { [FLOW_HEADER]: await sealForHeader(acme) },
    });
    expect(await getRequestAccount(start)).toEqual(acme);
    const forged = new NextRequest('http://app.example/api/auth/signin/oidc', {
      method: 'POST',
      headers: { [FLOW_HEADER]: await sealForFlow(acme, 's') },
    });
    expect(await getRequestAccount(forged)).toBeNull();
  });

  it('is the deployment everywhere else', async () => {
    const sealed = await sealForHeader(acme);
    const session = new NextRequest('http://app.example/api/auth/session', {
      headers: {
        [FLOW_HEADER]: sealed,
        cookie: 'husonym.login-account=acme',
      },
    });
    expect(await getRequestAccount(session)).toBeNull();
    expect(await getRequestAccount(undefined)).toBeNull();
  });
});
