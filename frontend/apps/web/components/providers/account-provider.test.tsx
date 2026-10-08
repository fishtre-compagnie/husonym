import { create } from '@bufbuild/protobuf';
import { TransportProvider, useQuery } from '@connectrpc/connect-query';
import {
  GetSystemInformationResponseSchema,
  GetUserAccountsResponseSchema,
  UserAccountSchema,
  UserAccountService,
  UserAccountType,
} from '@husonym/sdk';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import {
  act,
  ComponentProps,
  ReactElement,
  useEffect,
  useSyncExternalStore,
} from 'react';
import { createRoot, Root } from 'react-dom/client';
import AccountProvider, { useAccount } from './account-provider';

// The real provider, react-query and storages, over an API, a router and a session
// that the test holds. Nothing is rendered: the tree has no host element.

// ---- a window without a document
// Enough for the storages of usehooks-ts and for react-query to take itself for a
// browser. react-query looks for a window when it is loaded: the window is set up as
// the first thing that asks for react-query does, which this mock is hoisted to catch.
jest.mock('@tanstack/react-query', () => {
  class MemoryStorage {
    private readonly items = new Map<string, string>();
    getItem(key: string): string | null {
      return this.items.get(key) ?? null;
    }
    setItem(key: string, value: string): void {
      this.items.set(key, String(value));
    }
    removeItem(key: string): void {
      this.items.delete(key);
    }
    clear(): void {
      this.items.clear();
    }
  }
  class MemoryStorageEvent extends Event {
    readonly key: string | undefined;
    constructor(type: string, init?: { key?: string }) {
      super(type);
      this.key = init?.key;
    }
  }
  Object.assign(globalThis, {
    window: Object.assign(new EventTarget(), {
      localStorage: new MemoryStorage(),
      sessionStorage: new MemoryStorage(),
      HTMLIFrameElement: class {},
      StorageEvent: MemoryStorageEvent,
    }),
    StorageEvent: MemoryStorageEvent,
    IS_REACT_ACT_ENVIRONMENT: true,
  });
  return jest.requireActual('@tanstack/react-query');
});

// ---- the router of next, as a store
const nav = {
  path: '/',
  pushes: [] as string[],
  listeners: new Set<() => void>(),
  go(path: string): void {
    nav.path = path;
    nav.listeners.forEach((l) => l());
  },
  // Only /<account>/... pages carry the param.
  account(): string | undefined {
    return nav.path.split('/')[1] || undefined;
  },
};
jest.mock('next/navigation', () => ({
  useParams: () => {
    const account = useSyncExternalStore(
      (l: () => void) => {
        nav.listeners.add(l);
        return () => nav.listeners.delete(l);
      },
      () => nav.account()
    );
    return account ? { account } : {};
  },
  useRouter: () => ({
    push(path: string) {
      nav.pushes.push(path);
      nav.go(path);
    },
  }),
}));
jest.mock('next-auth/react', () => ({
  useSession: () => ({ status: 'authenticated' }),
}));

// ---- the API
interface Account {
  id: string;
  name: string;
  type: UserAccountType;
}
const PERSONAL = { id: 'p', name: 'personal', type: UserAccountType.PERSONAL };
const ORGANIZATION = { id: 'o', name: 'acme', type: UserAccountType.TEAM };

const api = {
  // Whether whoami passed: before it, the API knows no account of the user.
  entered: false,
  whoamiFailure: undefined as string | undefined,
  whoamiDelay: 30,
  accounts: [] as Account[],
  accountsDelay: 5,
  organizationId: undefined as string | undefined,
  // What the entry does to the instance.
  onEnter: (): void => {},
};
const sleep = (ms: number): Promise<void> =>
  new Promise((resolve) => setTimeout(resolve, ms));

// What an answer holds is decided when the call is served; then it travels.
const transport = {
  async unary(method: { name: string }) {
    const message = await (async () => {
      if (method.name === 'GetUserAccounts') {
        const accounts = api.entered ? [...api.accounts] : [];
        await sleep(api.accountsDelay);
        return create(GetUserAccountsResponseSchema, {
          accounts: accounts.map((a) => create(UserAccountSchema, a)),
        });
      }
      const organizationId = api.organizationId;
      await sleep(5);
      return create(GetSystemInformationResponseSchema, {
        instanceOrganizationAccountId: organizationId,
      });
    })();
    return { stream: false, method, message };
  },
} as unknown as ComponentProps<typeof TransportProvider>['transport'];

globalThis.fetch = (async (url: string) => {
  const answer = (status: number, body: unknown): unknown => ({
    ok: status < 400,
    status,
    json: async () => body,
  });
  if (url === '/api/config') {
    return answer(200, { isAuthEnabled: true });
  }
  await sleep(api.whoamiDelay);
  if (api.whoamiFailure) {
    return answer(500, { message: api.whoamiFailure });
  }
  api.entered = true;
  api.onEnter();
  return answer(200, { userId: 'u' });
}) as unknown as typeof fetch;

// ---- what the app sees of the provider
let context: ReturnType<typeof useAccount>;
let errorsTold: string[] = [];
function Probe(): ReactElement | null {
  const value = useAccount();
  // After every render that reaches the page, as the page would show it.
  useEffect(() => {
    context = value;
    if (value.entryError) {
      errorsTold.push(value.entryError);
    }
  });
  return null;
}
// As the header does: reads the accounts and the system information without waiting.
function Header(): ReactElement | null {
  useQuery(UserAccountService.method.getUserAccounts);
  useQuery(UserAccountService.method.getSystemInformation);
  return null;
}

let root: Root;
let queryClient: QueryClient;
async function mount(): Promise<void> {
  const container = {
    nodeType: 1,
    nodeName: 'DIV',
    tagName: 'DIV',
    namespaceURI: 'http://www.w3.org/1999/xhtml',
    textContent: '',
    addEventListener() {},
    removeEventListener() {},
    ownerDocument: {
      addEventListener() {},
      removeEventListener() {},
      defaultView: window,
    },
  } as unknown as Element;
  // As the client of the app, but for the time between two tries.
  queryClient = new QueryClient({
    defaultOptions: { queries: { retryDelay: 15 } },
  });
  root = createRoot(container);
  await act(async () => {
    root.render(
      <QueryClientProvider client={queryClient}>
        <TransportProvider transport={transport}>
          <AccountProvider>
            <Header />
            <Probe />
          </AccountProvider>
        </TransportProvider>
      </QueryClientProvider>
    );
  });
}
async function settle(ms = 300): Promise<void> {
  await act(async () => {
    await sleep(ms);
  });
}
function stored(): (string | null)[] {
  return [
    window.sessionStorage.getItem('account'),
    window.localStorage.getItem('account'),
  ];
}

beforeEach(() => {
  errorsTold = [];
  nav.pushes = [];
  nav.go('/');
  window.sessionStorage.clear();
  window.localStorage.clear();
  Object.assign(api, {
    entered: false,
    whoamiFailure: undefined,
    whoamiDelay: 30,
    accounts: [],
    accountsDelay: 5,
    organizationId: undefined,
    onEnter: () => {},
  });
});
afterEach(async () => {
  await act(async () => root.unmount());
  queryClient.clear();
});

describe('the account provider', () => {
  it('lands the first person of a new instance in the organization, and knows it is one', async () => {
    api.onEnter = () => {
      api.accounts = [ORGANIZATION];
      api.organizationId = 'o';
    };
    await mount();
    await settle();

    expect(context.account?.name).toBe('acme');
    expect(errorsTold).toEqual([]);
    expect(stored()).toEqual(['"acme"', '"acme"']);
    // The system information was asked before the entry made the organization: it is
    // asked again, for the header and the settings to know there is one.
    const systemInfo = queryClient
      .getQueryCache()
      .findAll()
      .find((q) => JSON.stringify(q.queryKey).includes('GetSystemInformation'));
    expect(systemInfo?.state.data).toMatchObject({
      instanceOrganizationAccountId: 'o',
    });
  });

  it('opens the organization the entry brings into, not the account held before', async () => {
    // A person the API knows already: the header is answered their personal account
    // before the entry adds the organization.
    api.entered = true;
    api.accounts = [PERSONAL];
    api.organizationId = 'o';
    api.onEnter = () => {
      api.accounts = [PERSONAL, ORGANIZATION];
    };
    await mount();
    await settle();

    expect(context.account?.name).toBe('acme');
  });

  it('falls to the default account when the name remembered matches none', async () => {
    api.accounts = [ORGANIZATION];
    api.organizationId = 'o';
    window.localStorage.setItem('account', '"personal"');
    nav.go('/personal/jobs');
    await mount();
    await settle();

    expect(context.account?.name).toBe('acme');
    expect(nav.pushes).toEqual(['/acme/jobs']);
    expect(stored()).toEqual(['"acme"', '"acme"']);
    expect(errorsTold).toEqual([]);
  });

  it('tells a user without account, and only once the entry passed', async () => {
    api.whoamiDelay = 150;
    await mount();
    // The header was answered an empty list already; the entry is still on its way.
    await settle(80);
    expect(errorsTold).toEqual([]);

    await settle();
    expect(context.entryError).toBe(
      'You are signed in, but you have no account on this instance.'
    );
    expect(context.account).toBeUndefined();
    expect(nav.pushes).toEqual([]);
  });

  it('does not take a list served before the entry and received after it for no account', async () => {
    api.accountsDelay = 120;
    api.whoamiDelay = 10;
    api.onEnter = () => {
      api.accounts = [ORGANIZATION];
      api.organizationId = 'o';
    };
    await mount();
    await settle(600);

    expect(errorsTold).toEqual([]);
    expect(context.account?.name).toBe('acme');
  });

  it('follows an account that changes its type and keeps its name', async () => {
    api.accounts = [PERSONAL];
    nav.go('/personal/settings/organization');
    await mount();
    await settle();
    expect(context.account?.type).toBe(UserAccountType.PERSONAL);

    // The personal account is made the organization, under the name it had.
    api.accounts = [{ ...PERSONAL, type: UserAccountType.TEAM }];
    api.organizationId = 'p';
    await act(async () => context.mutateUserAccount());
    await settle();

    expect(context.account?.type).toBe(UserAccountType.TEAM);
    expect(nav.pushes).toEqual([]);
  });

  it('tells an entry that fails, and recovers on a retry', async () => {
    api.whoamiFailure = 'unable to enter the organization of this instance';
    api.accounts = [PERSONAL];
    await mount();
    await settle(600);
    expect(context.entryError).toBe(
      'unable to enter the organization of this instance'
    );
    expect(context.account).toBeUndefined();

    api.whoamiFailure = undefined;
    await act(async () => context.retryEntry());
    await settle();
    expect(context.entryError).toBeUndefined();
    expect(context.account?.name).toBe('personal');
  });
});
