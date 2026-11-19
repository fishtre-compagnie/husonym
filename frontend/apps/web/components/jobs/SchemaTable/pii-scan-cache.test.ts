import { oldestScan, scanStillHolds } from './pii-scan-cache';

const col = (column: string, dataType: string) => ({ column, dataType });

describe('scanStillHolds', () => {
  const scanned = [col('id', 'integer'), col('email', 'varchar(255)')];

  it('tient pour une table inchangée, quel que soit l’ordre des colonnes', () => {
    expect(
      scanStillHolds(scanned, [
        col('email', 'varchar(255)'),
        col('id', 'integer'),
      ])
    ).toBe(true);
  });

  it('ne tient plus quand une colonne est ajoutée', () => {
    expect(
      scanStillHolds(scanned, [...scanned, col('phone', 'varchar(20)')])
    ).toBe(false);
  });

  it('ne tient plus quand une colonne est retirée ou renommée', () => {
    expect(scanStillHolds(scanned, [col('id', 'integer')])).toBe(false);
    expect(
      scanStillHolds(scanned, [
        col('id', 'integer'),
        col('mail', 'varchar(255)'),
      ])
    ).toBe(false);
  });

  it('ne tient plus quand le type d’une colonne change', () => {
    expect(
      scanStillHolds(scanned, [
        col('id', 'bigint'),
        col('email', 'varchar(255)'),
      ])
    ).toBe(false);
  });

  it('ne tient pas pour une table dont on ignore les colonnes', () => {
    expect(scanStillHolds(scanned, undefined)).toBe(false);
    expect(scanStillHolds(scanned, [])).toBe(false);
  });
});

describe('oldestScan', () => {
  it('rend la date la plus ancienne, et rien sans date', () => {
    const a = new Date('2026-11-10T10:00:00Z');
    const b = new Date('2026-11-18T10:00:00Z');
    expect(oldestScan([b, undefined, a])).toEqual(a);
    expect(oldestScan([undefined])).toBeUndefined();
  });
});
