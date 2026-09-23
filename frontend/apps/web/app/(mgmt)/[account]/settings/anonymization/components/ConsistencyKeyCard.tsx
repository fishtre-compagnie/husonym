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
      toast.success('Consistency key replaced');
    } catch (err) {
      toast.error('Unable to replace the consistency key', {
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
        <AlertTitle>Unable to read the settings of this account</AlertTitle>
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
        <CardTitle>Consistency key</CardTitle>
        <CardDescription>
          Both anonymization engines derive their deterministic outputs from it:
          the same input value turns into the same output, within the
          consistency scope a job sets. Replacing it changes every output of
          this account.
        </CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-6">
        <KeyState setting={setting} />
        <div className="flex flex-col gap-2">
          <Label htmlFor="consistency-key">Replace with a given key</Label>
          <p className="text-muted-foreground text-sm">
            A key is generated for the account on its first run; you give one
            only to take over the key of a deployment you are replacing, or to
            replay outputs produced elsewhere.
          </p>
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
                  Replace
                </Button>
              }
              headerText="Replace the consistency key?"
              description={
                setting
                  ? 'Every destination this account has already filled stops matching what a new run writes: the same source value will be anonymized into something else. There is no way back to the previous key unless you kept it.'
                  : 'This account will stop deriving from the key of the deployment and use the one you give. Destinations it has already filled stop matching what a new run writes.'
              }
              buttonText="Replace the key"
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
  if (!setting) {
    return (
      <Alert>
        <AlertTitle>This account has no key of its own</AlertTitle>
        <AlertDescription>
          Its runs derive from the key of the deployment
          (ANONYMIZATION_CONSISTENCY_KEY), shared with every other account.
          Where that variable is not set, the account is given a key of its own
          on its first run.
        </AlertDescription>
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
        <span className="text-sm font-medium">Fingerprint</span>
        <code className="rounded bg-muted px-2 py-0.5 font-mono text-sm">
          {fingerprint || '—'}
        </code>
      </div>
      <p className="text-muted-foreground text-sm">
        {drawn ? 'Generated for this account' : 'Given'}
        {at ? ` on ${at}` : ''}. The key itself never leaves the API: the
        fingerprint is there to tell two keys apart — whether two deployments
        share one, or whether a replacement really took.
      </p>
    </div>
  );
}

function SettingsUnavailableAlert(): ReactElement {
  return (
    <Alert variant="warning">
      <AlertTitle>This deployment holds no account settings</AlertTitle>
      <AlertDescription>
        Settings carry secrets, so they are only kept where the API can encrypt
        one. Set HUSONYM_SYM_ENCRYPTION_PASSWORD on the API to give each account
        a key of its own; until then, runs derive from
        ANONYMIZATION_CONSISTENCY_KEY, shared by every account.
      </AlertDescription>
    </Alert>
  );
}
