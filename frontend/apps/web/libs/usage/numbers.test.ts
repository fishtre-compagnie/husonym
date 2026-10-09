import { countFormat, formatCount } from './numbers';

type Global = 'window' | 'navigator';

// Runs `read` as a browser whose languages are the ones given would, then puts back
// what was there: the tests run where there is no browser.
function inBrowser<T>(languages: readonly string[], read: () => T): T {
  const before: Record<Global, PropertyDescriptor | undefined> = {
    window: Object.getOwnPropertyDescriptor(globalThis, 'window'),
    navigator: Object.getOwnPropertyDescriptor(globalThis, 'navigator'),
  };
  const set = (name: Global, value: unknown): void => {
    Object.defineProperty(globalThis, name, { value, configurable: true });
  };
  set('window', {});
  set('navigator', { languages });
  try {
    return read();
  } finally {
    for (const name of ['window', 'navigator'] as const) {
      const descriptor = before[name];
      if (descriptor) {
        Object.defineProperty(globalThis, name, descriptor);
      } else {
        Reflect.deleteProperty(globalThis, name);
      }
    }
  }
}

describe('formatCount', () => {
  it('groups the digits', () => {
    expect(formatCount(BigInt(0))).toBe('0');
    expect(formatCount(BigInt(999))).toBe('999');
    expect(formatCount(BigInt(1234567))).toBe('1,234,567');
    expect(formatCount(1234567)).toBe('1,234,567');
  });

  it('keeps every digit of a number a float cannot hold', () => {
    expect(formatCount(BigInt('9007199254740993'))).toBe(
      '9,007,199,254,740,993'
    );
  });

  it('groups them the way of the language of the browser', () => {
    expect(inBrowser(['de-DE'], () => formatCount(BigInt(1234567)))).toBe(
      '1.234.567'
    );
    // French groups with a narrow no-break space.
    expect(inBrowser(['fr-FR'], () => formatCount(BigInt(1234567)))).toBe(
      '1 234 567'
    );
    expect(inBrowser(['en-US'], () => formatCount(1234567))).toBe('1,234,567');
  });

  it('follows the first language of the browser that it knows', () => {
    expect(inBrowser(['de-DE', 'en-US'], () => formatCount(1234567))).toBe(
      '1.234.567'
    );
  });

  it('keeps every digit in the language of the browser too', () => {
    expect(
      inBrowser(['de-DE'], () => formatCount(BigInt('9007199254740993')))
    ).toBe('9.007.199.254.740.993');
  });

  it('does not need a browser: the server groups the one way', () => {
    expect(typeof window).toBe('undefined');
    expect(formatCount(1234567)).toBe('1,234,567');
  });

  it('leaves what was there once the browser is gone', () => {
    inBrowser(['de-DE'], () => formatCount(1));
    expect(typeof window).toBe('undefined');
    expect(formatCount(1234567)).toBe('1,234,567');
  });
});

describe('countFormat', () => {
  it('formats in the language of the browser, with the options given', () => {
    const format = (value: number): string =>
      countFormat({ maximumFractionDigits: 1 }).format(value);
    expect(inBrowser(['de-DE'], () => format(1.25))).toBe('1,3');
    expect(inBrowser(['en-US'], () => format(1.25))).toBe('1.3');
    expect(format(1.25)).toBe('1.3');
  });
});
