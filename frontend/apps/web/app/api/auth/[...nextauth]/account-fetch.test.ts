import { createServer, Server } from 'node:http';
import { AddressInfo } from 'node:net';
import { accountFetch } from './account-fetch';
import { isPublicAddress } from './public-address';

// The same cases as the backend's (safehttp): each blocked one is a way a request would
// otherwise reach something that is not on the internet.
describe('isPublicAddress', () => {
  it.each([
    '127.0.0.1',
    '127.42.7.9',
    '::1',
    '0.0.0.0',
    '::',
    '10.0.0.1',
    '172.20.13.4',
    '192.168.1.1',
    'fd00::1',
    '169.254.169.254',
    'fe80::1',
    '224.0.0.1',
    '100.64.0.1',
    '100.127.255.255',
    '::ffff:127.0.0.1',
    '::ffff:169.254.169.254',
    '::ffff:a9fe:a9fe',
    '2002:0a00:0001::',
    '64:ff9b::a9fe:a9fe',
    'not an address',
  ])('refuses %s', (address) => {
    expect(isPublicAddress(address)).toBe(false);
  });

  it.each([
    '93.184.216.34',
    '2606:2800:220:1:248:1893:25c8:1946',
    '100.128.0.1',
    '100.63.255.255',
  ])('lets %s through', (address) => {
    expect(isPublicAddress(address)).toBe(true);
  });
});

describe('accountFetch', () => {
  let server: Server;
  let port: number;
  beforeAll(async () => {
    server = createServer((_, res) => res.end('ok'));
    await new Promise<void>((resolve) => server.listen(0, resolve));
    port = (server.address() as AddressInfo).port;
  });
  afterAll(() => server.close());
  afterEach(() => {
    delete process.env.AUTH_ACCOUNT_ISSUER_ALLOW_PRIVATE;
  });

  it('refuses plain http', async () => {
    await expect(accountFetch('http://idp.example.com/')).rejects.toThrow(
      /https only/
    );
  });

  it('refuses a private address, literal or resolved', async () => {
    await expect(accountFetch(`https://127.0.0.1:${port}/`)).rejects.toThrow(
      /not on the public internet/
    );
    // localhost resolves to loopback: refused when the name is resolved, before any
    // connection is made.
    const refused = await accountFetch(`https://localhost:${port}/`).catch(
      (err: Error) => err
    );
    expect((refused as Error).cause).toEqual(
      expect.objectContaining({
        message: expect.stringMatching(/not on the public internet/),
      })
    );
  });

  it('reaches anything when the deployment allows it', async () => {
    process.env.AUTH_ACCOUNT_ISSUER_ALLOW_PRIVATE = 'true';
    const res = await accountFetch(`http://127.0.0.1:${port}/`);
    expect(await res.text()).toBe('ok');
  });
});
