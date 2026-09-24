'use client';
import ButtonText from '@/components/ButtonText';
import Spinner from '@/components/Spinner';
import { useAccount } from '@/components/providers/account-provider';
import { Button } from '@/components/ui/button';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import { useQuery } from '@connectrpc/connect-query';
import { ConnectionRole, ConnectionService } from '@husonym/sdk';
import { ReactElement } from 'react';
import ConnectionCheckList from './ConnectionCheckList';
import { ConnectionCheckResult, countFindings } from './useConnectionChecks';

interface Props {
  open: boolean;
  onOpenChange(open: boolean): void;
  results: ConnectionCheckResult[];
  isChecking: boolean;
  isSaving: boolean;
  onCheckAgain(): void;
  onSave(): void;
}

// ConnectionChecksDialog tells, before a job is saved, what its connections cannot do in
// their role. A blocking finding keeps the job from being saved; warnings let it be saved.
export default function ConnectionChecksDialog(props: Props): ReactElement {
  const {
    open,
    onOpenChange,
    results,
    isChecking,
    isSaving,
    onCheckAgain,
    onSave,
  } = props;
  const { account } = useAccount();
  const { data } = useQuery(
    ConnectionService.method.getConnections,
    { accountId: account?.id },
    { enabled: open && !!account?.id }
  );
  const names = new Map(
    (data?.connections ?? []).map((c) => [c.id, c.name] as const)
  );
  const { blocking, warnings } = countFindings(results);

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-3xl flex flex-col gap-4">
        <DialogHeader>
          <div className="flex flex-row items-center gap-2">
            <DialogTitle>Connection checks</DialogTitle>
            {isChecking ? <Spinner /> : null}
          </div>
          <DialogDescription>
            {getSummary(blocking, warnings)}
          </DialogDescription>
        </DialogHeader>
        <div className="flex max-h-[60vh] flex-col gap-5 overflow-y-auto pr-1">
          {results.map((result) => (
            <div
              key={`${result.target.role}-${result.target.connectionId}`}
              className="flex flex-col gap-2"
            >
              <h3 className="text-sm font-semibold">
                {result.target.role === ConnectionRole.SOURCE
                  ? 'Source'
                  : 'Destination'}{' '}
                · {names.get(result.target.connectionId) ?? 'connection'}
              </h3>
              <ConnectionCheckList
                checks={result.checks}
                unreachable={result.unreachable}
                allClearText="Nothing missing."
              />
            </div>
          ))}
        </div>
        <DialogFooter className="gap-2">
          <Button
            type="button"
            variant="outline"
            disabled={isChecking || isSaving}
            onClick={onCheckAgain}
          >
            Check again
          </Button>
          {blocking > 0 ? (
            <Button type="button" onClick={() => onOpenChange(false)}>
              Close
            </Button>
          ) : (
            <>
              <Button
                type="button"
                variant="outline"
                disabled={isSaving}
                onClick={() => onOpenChange(false)}
              >
                Cancel
              </Button>
              <Button
                type="button"
                disabled={isChecking || isSaving}
                onClick={onSave}
              >
                <ButtonText
                  leftIcon={isSaving ? <Spinner /> : undefined}
                  text={warnings > 0 ? 'Save anyway' : 'Save'}
                />
              </Button>
            </>
          )}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function getSummary(blocking: number, warnings: number): string {
  if (blocking > 0) {
    return `The job cannot be saved: ${plural(blocking, 'blocking finding')}. Grant what is missing, then check again.`;
  }
  if (warnings > 0) {
    return `${plural(warnings, 'warning')}: the run may stop on ${warnings > 1 ? 'them' : 'it'}.`;
  }
  return 'Nothing is missing any more.';
}

function plural(count: number, noun: string): string {
  return `${count} ${noun}${count > 1 ? 's' : ''}`;
}
