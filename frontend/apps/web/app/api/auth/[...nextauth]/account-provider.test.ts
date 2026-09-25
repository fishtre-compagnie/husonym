import { getSafeCallbackPath } from './account-provider';

describe('the path to come back to after signing in', () => {
  it('keeps a path of this app, with its query', () => {
    expect(getSafeCallbackPath('/invite?token=abc')).toBe('/invite?token=abc');
    expect(getSafeCallbackPath('/personal/jobs')).toBe('/personal/jobs');
  });

  it('refuses anything that leads elsewhere', () => {
    for (const value of [
      'https://evil.example/',
      '//evil.example/x',
      '/\\evil.example/x',
      '\\\\evil.example',
      'javascript:alert(1)',
      'evil.example',
      '',
      null,
      undefined,
    ]) {
      expect(getSafeCallbackPath(value)).toBeNull();
    }
  });
});
