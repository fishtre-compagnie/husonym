'use client';
import ButtonText from '@/components/ButtonText';
import Spinner from '@/components/Spinner';
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert';
import { Button } from '@/components/ui/button';
import { ReactElement, ReactNode } from 'react';
import { useAccount } from './account-provider';

interface Props {
  children: ReactNode;
}

// Stands in for the page when no account could be opened for the signed-in user: the
// pages wait for an account, and would wait for ever.
export default function AccountEntryGate(props: Props): ReactElement {
  const { children } = props;
  const { entryError, isRetryingEntry, retryEntry } = useAccount();
  if (!entryError) {
    return <>{children}</>;
  }
  return (
    <div className="flex justify-center mt-24">
      <Alert variant="destructive" className="max-w-xl">
        <AlertTitle>Unable to open your account</AlertTitle>
        <AlertDescription className="flex flex-col gap-4">
          <p>{entryError}</p>
          <div>
            <Button
              type="button"
              variant="outline"
              disabled={isRetryingEntry}
              onClick={() => retryEntry()}
            >
              <ButtonText
                leftIcon={isRetryingEntry ? <Spinner /> : undefined}
                text="Try again"
              />
            </Button>
          </div>
        </AlertDescription>
      </Alert>
    </div>
  );
}
