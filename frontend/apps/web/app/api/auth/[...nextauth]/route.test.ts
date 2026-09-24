import { NextRequest } from 'next/server';
import { fetchAccountLoginMethod } from './account-provider';
import { POST as handlePost } from './auth';
import { FLOW_HEADER, getRequestAccount, sealForFlow } from './login-flow';
import { GET, POST } from './route';

jest.mock('./auth', () => ({ GET: jest.fn(), POST: jest.fn() }));
jest.mock('./account-provider', () => ({
  ...jest.requireActual('./account-provider'),
  fetchAccountLoginMethod: jest.fn(),
}));

const acme = {
  issuer: 'https://idp.acme.example/realms/acme',
  clientId: 'husonym',
};
const authorize = (state: string | null) =>
  `https://idp.acme.example/auth?response_type=code&client_id=husonym${
    state ? `&state=${state}` : ''
  }`;

beforeEach(() => {
  process.env.AUTH_SECRET = 'test-secret';
  delete process.env.AUTH_URL;
  delete process.env.NEXTAUTH_URL;
  jest.mocked(fetchAccountLoginMethod).mockReset();
  jest.mocked(handlePost).mockReset();
});

function signIn(cookie: string, extra: Record<string, string> = {}) {
  return new NextRequest('http://app.example/api/auth/signin/oidc', {
    method: 'POST',
    headers: { cookie, ...extra },
  });
}

function setCookies(res: Response): string[] {
  return res.headers.getSetCookie();
}

describe('the start of a sign-in', () => {
  it('hands the configuration the account, and seals it for the flow', async () => {
    jest.mocked(fetchAccountLoginMethod).mockResolvedValue(acme);
    let seen: unknown;
    jest.mocked(handlePost).mockImplementation(async (req) => {
      seen = await getRequestAccount(req as NextRequest);
      return Response.json({ url: authorize('st') });
    });

    const res = await POST(
      signIn('husonym.login-account=acme', { [FLOW_HEADER]: 'forged' })
    );

    expect(fetchAccountLoginMethod).toHaveBeenCalledTimes(1);
    expect(seen).toEqual({ ...acme, slug: 'acme' });
    const flow = setCookies(res).find((c) =>
      c.startsWith('husonym.login-flow=')
    );
    expect(flow).toMatch(/Max-Age=900/);
    // The seal is for this flow's state.
    const value = flow!.split(';')[0].split('=')[1];
    const back = new NextRequest(
      'http://app.example/api/auth/callback/oidc?code=c&state=st',
      { headers: { cookie: `husonym.login-flow=${value}` } }
    );
    expect(await getRequestAccount(back)).toEqual({ ...acme, slug: 'acme' });
  });

  it('touches nothing when Auth.js started no flow', async () => {
    jest.mocked(fetchAccountLoginMethod).mockResolvedValue(acme);
    jest
      .mocked(handlePost)
      .mockResolvedValue(
        Response.redirect(
          'http://app.example/api/auth/error?error=MissingCSRF',
          302
        )
      );
    const res = await POST(signIn('husonym.login-account=acme'));
    expect(setCookies(res)).toEqual([]);
  });

  it('ends a sealed flow when a sign-in for no account starts', async () => {
    jest
      .mocked(handlePost)
      .mockResolvedValue(Response.json({ url: authorize(null) }));
    const res = await POST(signIn(''));
    expect(setCookies(res)).toEqual([
      expect.stringMatching(/^husonym\.login-flow=; .*Max-Age=0/),
    ]);
    expect(fetchAccountLoginMethod).not.toHaveBeenCalled();
  });

  it('seals under the __Host- prefix over https', async () => {
    process.env.AUTH_URL = 'https://app.example';
    jest.mocked(fetchAccountLoginMethod).mockResolvedValue(acme);
    jest
      .mocked(handlePost)
      .mockResolvedValue(Response.json({ url: authorize('st') }));
    const res = await POST(signIn('husonym.login-account=acme'));
    expect(setCookies(res)[0]).toMatch(
      /^__Host-husonym\.login-flow=.*; Path=\/; .*; Secure$/
    );
  });
});

describe('a callback', () => {
  const account = { ...acme, slug: 'acme' };

  it('ends the flow it is for', async () => {
    const { GET: handleGet } = jest.requireMock('./auth');
    handleGet.mockResolvedValue(new Response(null, { status: 302 }));
    const sealed = await sealForFlow(account, 'st');
    const res = await GET(
      new NextRequest(
        'http://app.example/api/auth/callback/oidc?code=c&state=st',
        { headers: { cookie: `husonym.login-flow=${sealed}` } }
      )
    );
    expect(setCookies(res)).toEqual([
      expect.stringMatching(/^husonym\.login-flow=; /),
      expect.stringMatching(/^husonym\.login-account=; /),
    ]);
  });

  it('leaves alone a flow it is not for', async () => {
    const { GET: handleGet } = jest.requireMock('./auth');
    handleGet.mockResolvedValue(new Response(null, { status: 302 }));
    const sealed = await sealForFlow(account, 'st');
    const res = await GET(
      new NextRequest(
        'http://app.example/api/auth/callback/oidc?code=c&state=other',
        { headers: { cookie: `husonym.login-flow=${sealed}` } }
      )
    );
    expect(setCookies(res)).toEqual([]);
  });
});
