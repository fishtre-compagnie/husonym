'use client';

import { Pill } from '@/components/ds';
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert';
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card';
import { Skeleton } from '@/components/ui/skeleton';
import { Table, TableBody, TableCell, TableRow } from '@/components/ui/table';
import {
  featureLabel,
  FeatureRow,
  featureRows,
  featureUseNote,
  LICENSE_FEATURES,
} from '@/libs/license/license';
import { getErrorMessage } from '@/util/util';
import { GetLicenseUsageResponse, SystemLicense } from '@husonym/sdk';
import { useTranslations } from 'next-intl';
import { ReactElement } from 'react';

interface Props {
  license?: SystemLicense;
  usage?: GetLicenseUsageResponse;
  isLoading: boolean;
  // Why the usage of the account could not be read. What the license includes is
  // still shown then.
  usageError: unknown;
}

export default function LicenseFeaturesCard(props: Props): ReactElement {
  const { license, usage, isLoading, usageError } = props;
  const t = useTranslations('settings.license.features');

  if (isLoading) {
    return <Skeleton className="w-full h-64" />;
  }

  const rows = featureRows(
    LICENSE_FEATURES,
    license,
    usage?.featuresInUse ?? []
  );
  const blocking = rows.filter((row) => row.blocking);
  const others = rows.filter((row) => !row.blocking);
  // A license that is not in force includes nothing: only what the account uses is
  // worth a row then, rather than every feature marked as missing.
  const inForce = !!license?.isValid;
  const shown = [
    ...blocking,
    ...(inForce ? others : others.filter((row) => row.inUse)),
  ];

  return (
    <Card>
      <CardHeader>
        <CardTitle>{t('title')}</CardTitle>
        <CardDescription>{t('description')}</CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        {!!usageError && (
          <Alert variant="warning">
            <AlertTitle>{t('usageError')}</AlertTitle>
            <AlertDescription>{getErrorMessage(usageError)}</AlertDescription>
          </Alert>
        )}
        {!inForce && <p className="text-sm">{t('noneInForce')}</p>}
        {blocking.length > 0 && (
          <p className="text-sm font-medium text-destructive">
            {t('blocking')}
          </p>
        )}
        {shown.length > 0 && (
          <Table>
            <TableBody>
              {shown.map((row) => (
                <FeatureTableRow key={row.name} row={row} />
              ))}
            </TableBody>
          </Table>
        )}
        <p className="text-muted-foreground text-sm">{t('notTracked')}</p>
      </CardContent>
    </Card>
  );
}

function FeatureTableRow({ row }: { row: FeatureRow }): ReactElement {
  const t = useTranslations('settings.license.features');
  return (
    <TableRow>
      <TableCell
        className={
          row.blocking ? 'font-medium text-destructive' : 'font-medium'
        }
      >
        {featureLabel(row.name)}
      </TableCell>
      <TableCell>
        {row.allowed ? (
          <Pill tone="ok">{t('included')}</Pill>
        ) : (
          <Pill tone={row.blocking ? 'danger' : 'quiet'}>
            {t('notIncluded')}
          </Pill>
        )}
      </TableCell>
      <TableCell className="text-muted-foreground">
        {featureUseNote(row)}
      </TableCell>
    </TableRow>
  );
}
