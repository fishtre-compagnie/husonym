'use client';
import FindingList, { FindingItem } from '@/components/findings/FindingList';
import { ConnectionCheck } from '@husonym/sdk';
import { CheckCircledIcon } from '@radix-ui/react-icons';
import { ReactElement } from 'react';
import { isBlocking } from './useConnectionChecks';

interface Props {
  checks: ConnectionCheck[];
  // Why the connection could not be asked, if it could not.
  unreachable?: string;
  // What is said when nothing is missing.
  allClearText: string;
}

// ConnectionCheckList shows, one per line, what a connection cannot do in its role: blocking
// findings first, each with what is missing and the statement that grants it, if any.
export default function ConnectionCheckList(props: Props): ReactElement {
  const { checks, unreachable, allClearText } = props;

  if (checks.length === 0 && !unreachable) {
    return (
      <div className="flex flex-row items-center gap-2 text-sm text-green-800 dark:text-green-400">
        <CheckCircledIcon className="h-4 w-4 shrink-0" />
        <span>{allClearText}</span>
      </div>
    );
  }
  const findings: FindingItem[] = checks.map((check, index) => ({
    key: `${check.kind}-${check.table}-${index}`,
    level: isBlocking(check) ? 'blocking' : 'warning',
    table: check.table,
    message: check.message,
    missing: check.missing,
    remedy: check.remedy,
  }));
  if (unreachable) {
    findings.unshift({
      key: 'unreachable',
      level: 'warning',
      message: `Could not be checked from the API: ${unreachable}. The run checks it again at its start.`,
    });
  }
  return <FindingList findings={findings} />;
}
