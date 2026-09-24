'use client';
import { CopyButton } from '@/components/CopyButton';
import { Badge } from '@/components/ui/badge';
import { ConnectionCheck } from '@husonym/sdk';
import {
  CheckCircledIcon,
  CrossCircledIcon,
  ExclamationTriangleIcon,
} from '@radix-ui/react-icons';
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
  const ordered = [...checks].sort(
    (a, b) => Number(isBlocking(b)) - Number(isBlocking(a))
  );
  return (
    <ul className="flex flex-col divide-y rounded-lg border">
      {unreachable ? (
        <Finding
          blocking={false}
          label="Warning"
          message={`Could not be checked from the API: ${unreachable}. The run checks it again at its start.`}
          missing={[]}
        />
      ) : null}
      {ordered.map((check, index) => (
        <Finding
          key={`${check.kind}-${check.table}-${index}`}
          blocking={isBlocking(check)}
          label={isBlocking(check) ? 'Blocking' : 'Warning'}
          table={check.table}
          message={check.message}
          missing={check.missing}
          remedy={check.remedy}
        />
      ))}
    </ul>
  );
}

interface FindingProps {
  blocking: boolean;
  label: string;
  table?: string;
  message: string;
  missing: string[];
  remedy?: string;
}

function Finding(props: FindingProps): ReactElement {
  const { blocking, label, table, message, missing, remedy } = props;
  return (
    <li className="flex flex-row gap-3 p-3 text-sm">
      {blocking ? (
        <CrossCircledIcon className="mt-0.5 h-4 w-4 shrink-0 text-red-700 dark:text-red-400" />
      ) : (
        <ExclamationTriangleIcon className="mt-0.5 h-4 w-4 shrink-0 text-orange-700 dark:text-orange-400" />
      )}
      <div className="flex min-w-0 flex-col gap-2">
        <div className="flex flex-row flex-wrap items-center gap-2">
          <span
            className={
              blocking
                ? 'font-medium text-red-800 dark:text-red-300'
                : 'font-medium text-orange-800 dark:text-orange-300'
            }
          >
            {label}
          </span>
          {table ? (
            <code className="rounded-sm bg-muted px-1 text-xs">{table}</code>
          ) : null}
        </div>
        <p className="break-words">{message}</p>
        {missing.length > 0 ? (
          <div className="flex flex-row flex-wrap gap-1">
            {missing.map((item) => (
              <Badge key={item} variant="darkOutline">
                {item}
              </Badge>
            ))}
          </div>
        ) : null}
        {remedy ? (
          <div className="flex flex-row items-start gap-2">
            <pre className="min-w-0 flex-1 overflow-x-auto rounded-md bg-muted p-2 text-xs">
              {remedy}
            </pre>
            <CopyButton
              buttonVariant="outline"
              textToCopy={remedy}
              onHoverText="Copy the statement"
              onCopiedText="Copied"
            />
          </div>
        ) : null}
      </div>
    </li>
  );
}
