import { DEFAULT_CRON_STRING } from '../schedule';
import { scheduleLabel } from './schedule-label';

describe('scheduleLabel', () => {
  it('says manual for a job without a schedule', () => {
    const manual = { isCron: false, key: 'manual' };
    expect(scheduleLabel(undefined)).toEqual(manual);
    expect(scheduleLabel('')).toEqual(manual);
    expect(scheduleLabel(DEFAULT_CRON_STRING)).toEqual(manual);
  });

  it('says the common shapes in words', () => {
    expect(scheduleLabel('0 3 * * *')).toEqual({
      isCron: false,
      key: 'dailyAt',
      values: { time: '03:00' },
    });
    expect(scheduleLabel('30 6 * * 1')).toEqual({
      isCron: false,
      key: 'everyDayAt',
      values: { day: '1', time: '06:30' },
    });
    expect(scheduleLabel('0 6 * * 1-5')).toEqual({
      isCron: false,
      key: 'weekdaysAt',
      values: { time: '06:00' },
    });
    expect(scheduleLabel('0 2 15 * *')).toEqual({
      isCron: false,
      key: 'monthlyAt',
      values: { day: '15', time: '02:00' },
    });
    expect(scheduleLabel('15 * * * *')).toEqual({
      isCron: false,
      key: 'hourly',
    });
    expect(scheduleLabel('*/10 * * * *')).toEqual({
      isCron: false,
      key: 'everyMinutes',
      values: { count: '10' },
    });
    expect(scheduleLabel('0 */6 * * *')).toEqual({
      isCron: false,
      key: 'everyHours',
      values: { count: '6' },
    });
    expect(scheduleLabel('@daily')).toEqual({ isCron: false, key: 'daily' });
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
