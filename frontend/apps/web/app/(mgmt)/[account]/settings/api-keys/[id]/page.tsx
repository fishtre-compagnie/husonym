'use client';
import { Pill } from '@/components/ds';
import { ApiKeyValueSessionStore } from '@/app/(mgmt)/[account]/new/api-key/NewApiKeyForm';
import { permissionLabel } from '@/app/(mgmt)/[account]/new/api-key/permissions';
import ButtonText from '@/components/ButtonText';
import { CopyButton } from '@/components/CopyButton';
import { useAccount } from '@/components/providers/account-provider';
import SkeletonForm from '@/components/skeleton/SkeletonForm';
import TruncatedText from '@/components/TruncatedText';
import { PageProps } from '@/components/types';
import { Alert, AlertTitle } from '@/components/ui/alert';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { formatDateTime } from '@/util/util';
import { timestampDate } from '@bufbuild/protobuf/wkt';
import { useQuery } from '@connectrpc/connect-query';
import { AccountApiKey, ApiKeyService } from '@husonym/sdk';
import { InfoCircledIcon, ReloadIcon } from '@radix-ui/react-icons';
import Error from 'next/error';
import Link from 'next/link';
import { useRouter } from 'next/navigation';
import { ReactElement, use, useEffect, useState } from 'react';
import { useSessionStorage } from 'usehooks-ts';
import RemoveAccountApiKeyButton from './components/RemoveAccountApiKeyButton';

export default function AccountApiKeyPage(props: PageProps): ReactElement {
  const params = use(props.params);
  const id = params?.id ?? '';
  const router = useRouter();
  const { account } = useAccount();
  const { data, isLoading } = useQuery(
    ApiKeyService.method.getAccountApiKey,
    { id },
    { enabled: !!id }
  );
  const [sessionApiKeyValue] = useSessionStorage<
    ApiKeyValueSessionStore | undefined
  >(id, undefined);
  const [apiKeyValue, setApiKeyValue] = useState<string | undefined>(
    sessionApiKeyValue?.keyValue
  );
  // Don't persist the api key in session storage any longer than is necessary.
  useEffect(() => {
    if (!!sessionApiKeyValue) {
      window.sessionStorage.removeItem(id);
      setApiKeyValue(sessionApiKeyValue.keyValue);
    }
  }, [sessionApiKeyValue]);

  if (!id) {
    return <Error statusCode={404} />;
  }
  if (isLoading) {
    return (
      <div>
        <SkeletonForm />
      </div>
    );
  }
  if (!data?.apiKey) {
    return (
      <div className="mt-10">
        <Alert variant="destructive">
          <AlertTitle>{`Error: Unable to retrieve api key`}</AlertTitle>
        </Alert>
      </div>
    );
  }
  return (
    <div>
      <div className="flex flex-row flex-wrap items-end justify-between gap-4">
        <div className="flex min-w-0 flex-col justify-start gap-1">
          <h2 className="m-0">
            <TruncatedText
              text={`API Key: ${data.apiKey.name}`}
              align="start"
              maxWidth={500}
              truncatedContainerClassName="font-display text-xl font-semibold tracking-[-0.01em]"
            />
          </h2>
          <span className="font-mono text-xs text-muted-foreground">
            {data.apiKey.id}
          </span>
        </div>
        <div className="flex flex-row gap-2">
          <RemoveAccountApiKeyButton
            id={id}
            onDeleted={() => router.push(`/${account?.name}/settings/api-keys`)}
          />
          <Link href={`/${account?.name}/settings/api-keys/${id}/regenerate`}>
            <Button type="button">
              <ButtonText
                leftIcon={<ReloadIcon className="h-4 w-4" />}
                text="Regenerate"
              />
            </Button>
          </Link>
        </div>
      </div>
      <div className="mt-6">
        <ApiKeyDetails apiKey={data.apiKey} keyValue={apiKeyValue} />
      </div>
    </div>
  );
}

interface ApiKeyDetailsProps {
  apiKey: AccountApiKey;
  keyValue?: string;
}

function ApiKeyDetails(props: ApiKeyDetailsProps): ReactElement {
  const { apiKey, keyValue } = props;
  return (
    <div className="flex flex-col gap-3">
      {keyValue && (
        <Alert variant="success">
          <div className="flex flex-row items-center gap-3">
            <InfoCircledIcon />
            <div className="font-semibold">
              Make sure to copy this access token now as you will not be able to
              see it again!
            </div>
          </div>
        </Alert>
      )}
      <div className="flex flex-col gap-6 rounded-2xl border border-line-soft bg-card p-5">
        {keyValue && (
          <div className="flex flex-row gap-3">
            <Input value={keyValue} disabled={true} />
            <CopyButton
              buttonVariant="outline"
              textToCopy={keyValue ?? ''}
              onCopiedText="Success!"
              onHoverText="Copy the API key"
            />
          </div>
        )}
        <dl className="grid grid-cols-1 gap-x-6 gap-y-3 text-sm sm:grid-cols-[140px_1fr]">
          <dt className="text-muted-foreground">Created</dt>
          <dd className="m-0 tabular-nums">
            {formatDateTime(
              apiKey.createdAt ? timestampDate(apiKey.createdAt) : new Date()
            )}
          </dd>
          <dt className="text-muted-foreground">Updated</dt>
          <dd className="m-0 tabular-nums">
            {formatDateTime(
              apiKey.updatedAt ? timestampDate(apiKey.updatedAt) : new Date()
            )}
          </dd>
          <dt className="text-muted-foreground">Expires</dt>
          <dd className="m-0 tabular-nums">
            {formatDateTime(
              apiKey.expiresAt ? timestampDate(apiKey.expiresAt) : new Date()
            )}
          </dd>
          <dt className="text-muted-foreground">User ID</dt>
          <dd className="m-0 break-all font-mono text-xs">{apiKey.userId}</dd>
          <dt className="text-muted-foreground">Permissions</dt>
          <dd className="m-0 flex flex-row flex-wrap gap-1.5">
            {apiKey.permissions.length === 0 ? (
              <Pill tone="warn">None: the key can do nothing</Pill>
            ) : (
              apiKey.permissions.map((p) => (
                <Pill key={p} className="font-mono">
                  {permissionLabel(p)}
                </Pill>
              ))
            )}
          </dd>
        </dl>
      </div>
    </div>
  );
}
