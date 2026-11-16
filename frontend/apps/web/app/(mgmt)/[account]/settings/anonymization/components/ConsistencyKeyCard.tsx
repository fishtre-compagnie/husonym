'use client';

import ConfirmationDialog from '@/components/ConfirmationDialog';
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert';
import { Button } from '@/components/ui/button';
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Skeleton } from '@/components/ui/skeleton';
import { formatDateTime, getErrorMessage } from '@/util/util';
import { create } from '@bufbuild/protobuf';
import { timestampDate } from '@bufbuild/protobuf/wkt';
import { useMutation, useQuery } from '@connectrpc/connect-query';
import {
  AccountSetting,
  AccountSettingConfigSchema,
  AccountSettingService,
  Code,
  ConnectError,
} from '@husonym/sdk';
import { useTranslations } from 'next-intl';
import { ReactElement, useState } from 'react';
import { toast } from 'sonner';

// The field the fingerprint of the key is filed under, as protosecret names it: the path
// of the field it stands in for.
const KEY_FINGERPRINT = 'anonymization_consistency.derivation_key';

interface Props {
  accountId: string;
}

export default function ConsistencyKeyCard(props: Props): ReactElement {
  const { accountId } = props;
  const { data, isLoading, error, refetch } = useQuery(
    AccountSettingService.method.getAccountSettings,
    { accountId },
    { enabled: !!accountId, retry: false }
  );
  const { mutateAsync: setSetting } = useMutation(
    AccountSettingService.method.setAccountSetting
  );
  const [key, setKey] = useState('');
  const t = useTranslations('settings.anonymization.key');

  async function onReplace(): Promise<void> {
    try {
      await setSetting({
        accountId,
        config: create(AccountSettingConfigSchema, {
          config: {
            case: 'anonymizationConsistency',
            value: { derivationKey: key },
          },
        }),
      });
      setKey('');
      // The response carries the setting, but the fingerprint and the dates this card
      // shows come from the list: read it again rather than patch it in place.
      await refetch();
      toast.success(t('replaced'));
    } catch (err) {
      toast.error(t('replaceError'), {
        description: getErrorMessage(err),
      });
    }
  }

  if (isLoading) {
    return <Skeleton className="w-full h-32" />;
  }

  // A deployment with no encryption password holds no account settings at all: the API
  // says so rather than pretending the account simply has none.
  if (error instanceof ConnectError && error.code === Code.Unimplemented) {
    return <SettingsUnavailableAlert />;
  }
  if (error) {
    return (
      <Alert variant="destructive">
        <AlertTitle>{t('readError')}</AlertTitle>
        <AlertDescription>{getErrorMessage(error)}</AlertDescription>
      </Alert>
    );
  }

  const setting = data?.settings.find(
    (s) => s.config?.config.case === 'anonymizationConsistency'
  );

  return (
    <Card>
      <CardHeader>
        <CardTitle>{t('title')}</CardTitle>
        <CardDescription>{t('description')}</CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-6">
        <KeyState setting={setting} />
        <div className="flex flex-col gap-2">
          <Label htmlFor="consistency-key">{t('replaceLabel')}</Label>
          <p className="text-muted-foreground text-sm">{t('replaceHelp')}</p>
          <div className="flex flex-row gap-2">
            <Input
              id="consistency-key"
              type="password"
              autoComplete="off"
              value={key}
              onChange={(e) => setKey(e.target.value)}
              className="max-w-md font-mono"
            />
            <ConfirmationDialog
              trigger={
                <Button type="button" variant="destructive" disabled={!key}>
                  {t('replace')}
                </Button>
              }
              headerText={t('confirmHeader')}
              description={
                setting ? t('confirmWithKey') : t('confirmWithoutKey')
              }
              buttonText={t('confirmButton')}
              buttonVariant="destructive"
              onConfirm={onReplace}
            />
          </div>
        </div>
      </CardContent>
    </Card>
  );
}

function KeyState({ setting }: { setting?: AccountSetting }): ReactElement {
  const t = useTranslations('settings.anonymization.key');
  if (!setting) {
    return (
      <Alert>
        <AlertTitle>{t('noOwnKey')}</AlertTitle>
        <AlertDescription>{t('noOwnKeyDescription')}</AlertDescription>
      </Alert>
    );
  }

  const fingerprint = setting.secretFingerprints[KEY_FINGERPRINT];
  // No user behind it means a run drew it; somebody gave it otherwise. That is what
  // changes how the key should be read, far more than which person typed it.
  const drawn = !setting.createdByUserId;
  const at = setting.updatedAt
    ? formatDateTime(timestampDate(setting.updatedAt))
    : undefined;

  return (
    <div className="flex flex-col gap-1">
      <div className="flex flex-row items-center gap-2">
        <span className="text-sm font-medium">{t('fingerprint')}</span>
        <code className="rounded bg-muted px-2 py-0.5 font-mono text-sm">
          {fingerprint || '—'}
        </code>
      </div>
      <p className="text-muted-foreground text-sm">
        {drawn
          ? at
            ? t('generatedOn', { at })
            : t('generated')
          : at
            ? t('givenOn', { at })
            : t('given')}{' '}
        {t('fingerprintNote')}
      </p>
    </div>
  );
}

function SettingsUnavailableAlert(): ReactElement {
  const t = useTranslations('settings.anonymization.key');
  return (
    <Alert variant="warning">
      <AlertTitle>{t('unavailableTitle')}</AlertTitle>
      <AlertDescription>{t('unavailableDescription')}</AlertDescription>
    </Alert>
  );
}
