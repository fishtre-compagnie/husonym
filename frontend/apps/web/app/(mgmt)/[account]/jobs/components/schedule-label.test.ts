import { DEFAULT_CRON_STRING } from '../schedule';
import { scheduleLabel } from './schedule-label';

describe('scheduleLabel', () => {
  it('says manual for a job without a schedule', () => {
    expect(scheduleLabel(undefined).text).toBe('Manual');
    expect(scheduleLabel('').text).toBe('Manual');
    expect(scheduleLabel(DEFAULT_CRON_STRING).text).toBe('Manual');
  });

  it('says the common shapes in words', () => {
    expect(scheduleLabel('0 3 * * *').text).toBe('Daily at 03:00 UTC');
    expect(scheduleLabel('30 6 * * 1').text).toBe('Every Monday at 06:30 UTC');
    expect(scheduleLabel('0 6 * * 1-5').text).toBe('Weekdays at 06:00 UTC');
    expect(scheduleLabel('0 2 15 * *').text).toBe(
      'Monthly on day 15 at 02:00 UTC'
    );
    expect(scheduleLabel('15 * * * *').text).toBe('Hourly');
    expect(scheduleLabel('*/10 * * * *').text).toBe('Every 10 min');
    expect(scheduleLabel('0 */6 * * *').text).toBe('Every 6 h');
    expect(scheduleLabel('@daily').text).toBe('Daily at 00:00 UTC');
  });

  it('shows any other shape as the cron expression', () => {
    expect(scheduleLabel('0 3 * 2 *')).toEqual({
      text: '0 3 * 2 *',
      isCron: true,
    });
    expect(scheduleLabel('0 3,15 * * *')).toEqual({
      text: '0 3,15 * * *',
      isCron: true,
    });
  });
});
