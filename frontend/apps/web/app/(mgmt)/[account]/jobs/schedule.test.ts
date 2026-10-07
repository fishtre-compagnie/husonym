import { cronToClone, DEFAULT_CRON_STRING } from './schedule';

describe('cronToClone', () => {
  it('gives no schedule to the copy of an unscheduled job', () => {
    expect(cronToClone(DEFAULT_CRON_STRING)).toBe('');
  });

  it('keeps any other schedule', () => {
    expect(cronToClone('0 3 * * *')).toBe('0 3 * * *');
  });

  it('keeps an empty schedule empty', () => {
    expect(cronToClone('')).toBe('');
  });
});
