import { getSafePath } from './safe-path';

describe('the path to come back to after signing in', () => {
  it('keeps a path of this app, with its query', () => {
    expect(getSafePath('/invite?token=abc')).toBe('/invite?token=abc');
    expect(getSafePath('/personal/jobs')).toBe('/personal/jobs');
  });

  it('refuses anything that leads elsewhere', () => {
    for (const value of [
      'https://evil.example/',
      '//evil.example/x',
      '/\\evil.example/x',
      '\\\\evil.example',
      '/.//evil.example/x',
      '/..//evil.example',
      '/a/..//evil.example',
      '/%2e//evil.example',
      '/%2e%2e//evil.example',
      '/./\\evil.example',
      'javascript:alert(1)',
      'evil.example',
      '',
      null,
      undefined,
    ]) {
      expect(getSafePath(value)).toBeNull();
    }
  });
});
