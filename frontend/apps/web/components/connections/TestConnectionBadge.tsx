'use client';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import { getErrorMessage } from '@/util/util';
import {
  CheckConnectionConfigByIdResponse,
  CheckConnectionConfigResponse,
  ConnectionCheck,
} from '@husonym/sdk';
import { ArrowTopRightIcon, CheckCircledIcon } from '@radix-ui/react-icons';
import Link from 'next/link';
import { ReactElement, useState } from 'react';
import { MdErrorOutline } from 'react-icons/md';
import { TiWarningOutline } from 'react-icons/ti';
import ConnectionCheckList from './checks/ConnectionCheckList';
import { countChecks } from './checks/useConnectionChecks';

interface TestConnectionBadgeProps {
  validationResponse:
    | CheckConnectionConfigResponse
    | CheckConnectionConfigByIdResponse
    | undefined;
  connectionId: string | undefined;
  accountName: string;
}

export default function TestConnectionBadge(
  props: TestConnectionBadgeProps
): ReactElement {
  const { validationResponse, connectionId, accountName } = props;

  return (
    <ValidationResponseBadge
      validationResponse={validationResponse}
      accountName={accountName}
      connectionId={connectionId ?? ''}
    />
  );
}

interface ValidationResponseBadgeProps {
  validationResponse:
    | CheckConnectionConfigResponse
    | CheckConnectionConfigByIdResponse
    | undefined;
  accountName: string;
  connectionId: string;
}

function ValidationResponseBadge(
  props: ValidationResponseBadgeProps
): ReactElement | null {
  const { validationResponse, accountName, connectionId } = props;
  const url = `/${accountName}/connections/${connectionId}/permissions`;

  if (!validationResponse) {
    return null;
  }
  if (validationResponse.isConnected) {
    if (validationResponse.checks.length > 0) {
      return <FindingsBadge checks={validationResponse.checks} />;
    }
    if (validationResponse.privileges.length === 0) {
      return (
        <div className="inline-flex">
          <Link
            href={url}
            passHref
            target="_blank"
            className="flex flex-row items-center gap-2 rounded-full px-2.5 py-1 h-auto text-warn-fg border border-warn-line bg-warn-soft hover:brightness-110 transition"
          >
            <TiWarningOutline />
            <div className="text-nowrap text-xs pl-2 font-medium">
              Successfully connected - found no tables{' '}
              <span className="underline">More info</span>
            </div>
            <ArrowTopRightIcon />
          </Link>
        </div>
      );
    } else {
      return (
        <div className="flex flex-row items-center gap-2 rounded-full px-2.5 py-1 h-auto text-ok bg-ok-soft transition-colors">
          <CheckCircledIcon />
          <div className="text-nowrap text-xs font-medium">
            Successfully connected
          </div>
        </div>
      );
    }
  }
  return (
    <div className="inline-flex">
      <Link
        href={url}
        passHref
        target="_blank"
        className="flex flex-row items-center gap-2 rounded-xl px-2.5 py-1 h-auto text-danger-fg border border-danger-line bg-danger-soft hover:brightness-110 transition"
      >
        <MdErrorOutline />
        <div className="text-nowrap text-xs pl-2 font-medium">
          Connection Error - Unable to connect.{' '}
          <span className="underline">More info</span>
          <p>{getErrorMessage(validationResponse.connectionError)}</p>
        </div>
        <ArrowTopRightIcon />
      </Link>
    </div>
  );
}

interface FindingsBadgeProps {
  checks: ConnectionCheck[];
}

// FindingsBadge tells what the connection cannot do in its role at server level, and opens
// the findings. The job's tables are checked when the job is saved.
function FindingsBadge(props: FindingsBadgeProps): ReactElement {
  const { checks } = props;
  const [open, setOpen] = useState(false);
  const { blocking, warnings } = countChecks(checks);
  const parts = [
    blocking > 0 ? `${blocking} blocking` : '',
    warnings > 0 ? `${warnings} warning${warnings > 1 ? 's' : ''}` : '',
  ].filter((part) => !!part);
  const className =
    blocking > 0
      ? 'text-danger-fg border-danger-line bg-danger-soft hover:brightness-110'
      : 'text-warn-fg border-warn-line bg-warn-soft hover:brightness-110';
  return (
    <>
      <button
        type="button"
        onClick={() => setOpen(true)}
        className={`inline-flex flex-row items-center gap-2 rounded-full border px-2.5 py-1 h-auto transition ${className}`}
      >
        {blocking > 0 ? <MdErrorOutline /> : <TiWarningOutline />}
        <span className="text-nowrap text-xs font-medium">
          Connected · {parts.join(', ')}{' '}
          <span className="underline">Details</span>
        </span>
      </button>
      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent className="max-w-3xl flex flex-col gap-4">
          <DialogHeader>
            <DialogTitle>Connection checks</DialogTitle>
            <DialogDescription>
              What the connection cannot do in its role, at server level. The
              job&apos;s tables are checked when the job is saved.
            </DialogDescription>
          </DialogHeader>
          <ConnectionCheckList
            checks={checks}
            allClearText="Nothing missing."
          />
        </DialogContent>
      </Dialog>
    </>
  );
}
