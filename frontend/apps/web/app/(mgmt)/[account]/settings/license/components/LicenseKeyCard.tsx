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
import { Label } from '@/components/ui/label';
import { Textarea } from '@/components/ui/textarea';
import { formatDate, isKeyAlreadyInForce } from '@/libs/license/license';
import { getErrorMessage } from '@/util/util';
import { timestampDate } from '@bufbuild/protobuf/wkt';
import { useMutation } from '@connectrpc/connect-query';
import {
  Code,
  ConnectError,
  SystemLicense,
  UserAccountService,
} from '@husonym/sdk';
import { ReactElement, useState } from 'react';
import { toast } from 'sonner';

interface Props {
  accountId: string;
  // The license the page shows, to tell a key that is already the one in force.
  current?: SystemLicense;
  // Reads again what the page shows, once a key was taken.
  onInstalled(): Promise<unknown>;
}

export default function LicenseKeyCard(props: Props): ReactElement {
  const { accountId, current, onInstalled } = props;
  const { mutateAsync: setLicense } = useMutation(
    UserAccountService.method.setSystemLicense
  );
  const [key, setKey] = useState('');
  const [refusal, setRefusal] = useState<string>();

  async function onInstall(): Promise<void> {
    setRefusal(undefined);
    let installed: SystemLicense | undefined;
    try {
      // Sent as it was typed: the API cleans a key of what a paste adds around it.
      const resp = await setLicense({ accountId, key });
      installed = resp.license;
    } catch (err) {
      // Kept on the page rather than in a toast: a refusal gives a reason, dates
      // included, that is worth reading twice.
      setRefusal(refusalMessage(err));
      return;
    }
    setKey('');
    // The API answers the key in force pasted again like a new one: nothing was
    // installed then, and the page does not say that something was.
    toast.success(
      isKeyAlreadyInForce(current, installed)
        ? 'This key is already in force'
        : 'License key installed',
      { description: installedDescription(installed) }
    );
    // Outside of what catches a refusal: the key is installed by now, and a page that
    // fails to read itself again must not say it was not.
    await onInstalled();
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle>License key</CardTitle>
        <CardDescription>
          The key applies to the whole instance, every account included. The key
          in force is never shown here.
        </CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        {refusal && (
          <Alert variant="destructive">
            <AlertTitle>The license key was not installed</AlertTitle>
            <AlertDescription>{refusal}</AlertDescription>
          </Alert>
        )}
        <div className="flex flex-col gap-2">
          <Label htmlFor="license-key">Paste a new key</Label>
          <Textarea
            id="license-key"
            autoComplete="off"
            spellCheck={false}
            rows={5}
            value={key}
            onChange={(e) => setKey(e.target.value)}
            className="font-mono"
          />
        </div>
        <div>
          <ConfirmationDialog
            trigger={
              <Button type="button" disabled={!key.trim()}>
                Install key
              </Button>
            }
            headerText="Install this license key?"
            description="It replaces the key in force for the whole instance."
            buttonText="Install key"
            onConfirm={onInstall}
          />
        </div>
      </CardContent>
    </Card>
  );
}

function installedDescription(
  license: SystemLicense | undefined
): string | undefined {
  if (!license) {
    return undefined;
  }
  const parts: string[] = [];
  if (license.issuedTo) {
    parts.push(`Issued to ${license.issuedTo}`);
  }
  if (license.expiresAt) {
    parts.push(`expires on ${formatDate(timestampDate(license.expiresAt))}`);
  }
  return parts.join(', ') || undefined;
}

function refusalMessage(err: unknown): string {
  if (err instanceof ConnectError) {
    if (err.code === Code.PermissionDenied) {
      return 'An administrator of this account must install the license key.';
    }
    // The message of the API is written to be read as it is, and never holds the key.
    return err.rawMessage;
  }
  return getErrorMessage(err);
}
