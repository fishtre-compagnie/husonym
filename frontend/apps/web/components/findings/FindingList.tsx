'use client';
import { CopyButton } from '@/components/CopyButton';
import { Pill } from '@/components/ds';
import {
  CrossCircledIcon,
  ExclamationTriangleIcon,
  InfoCircledIcon,
} from '@radix-ui/react-icons';
import { ReactElement } from 'react';

// What a finding does: stop the run, perhaps stop it or copy wrong, or only tell what the
// run does.
export type FindingLevel = 'blocking' | 'warning' | 'information';

export interface FindingItem {
  key: string;
  level: FindingLevel;
  table?: string;
  columns?: string[];
  message: string;
  // What an account lacks: privileges, or names of absent columns.
  missing?: string[];
  // The statement that grants what is missing.
  remedy?: string;
}

const levelOrder: Record<FindingLevel, number> = {
  blocking: 0,
  warning: 1,
  information: 2,
};

const levelLabels: Record<FindingLevel, string> = {
  blocking: 'Blocking',
  warning: 'Warning',
  information: 'Note',
};

// FindingList shows findings one per line, the most serious first: what each is about,
// what is missing and the statement that grants it, if any. The statement can be copied
// only when it comes from the API as it answers: one read from storage may have been
// written by someone else than the worker, and is shown for reading.
export default function FindingList(props: {
  findings: FindingItem[];
  copyableRemedies?: boolean;
}): ReactElement {
  const copyable = props.copyableRemedies ?? true;
  const ordered = [...props.findings].sort(
    (a, b) => levelOrder[a.level] - levelOrder[b.level]
  );
  return (
    <ul className="flex flex-col divide-y divide-line-soft overflow-hidden rounded-xl border border-line-soft bg-card">
      {ordered.map((finding) => (
        <Finding key={finding.key} finding={finding} copyable={copyable} />
      ))}
    </ul>
  );
}

function Finding(props: {
  finding: FindingItem;
  copyable: boolean;
}): ReactElement {
  const { level, table, columns, message, missing, remedy } = props.finding;
  return (
    <li className="flex flex-row gap-3 p-3.5 text-sm">
      <LevelIcon level={level} />
      <div className="flex min-w-0 flex-col gap-2">
        <div className="flex flex-row flex-wrap items-center gap-2">
          <span className={`font-medium ${levelTextClass(level)}`}>
            {levelLabels[level]}
          </span>
          {table ? (
            <code className="rounded-md bg-surface-2 px-1.5 py-0.5 font-mono text-xs text-hi">
              {columns && columns.length > 0
                ? `${table} (${columns.join(', ')})`
                : table}
            </code>
          ) : null}
        </div>
        <p className="break-words">{message}</p>
        {missing && missing.length > 0 ? (
          <div className="flex flex-row flex-wrap gap-1">
            {missing.map((item) => (
              <Pill key={item} className="font-mono">
                {item}
              </Pill>
            ))}
          </div>
        ) : null}
        {remedy ? (
          <div className="flex flex-row items-start gap-2">
            {/* Wrapped: nothing of what would be copied is out of sight. */}
            <pre className="min-w-0 flex-1 whitespace-pre-wrap break-all rounded-lg border border-border bg-surface-2 p-2.5 font-mono text-xs">
              {remedy}
            </pre>
            {props.copyable ? (
              <CopyButton
                buttonVariant="outline"
                textToCopy={remedy}
                onHoverText="Copy the statement"
                onCopiedText="Copied"
              />
            ) : null}
          </div>
        ) : null}
      </div>
    </li>
  );
}

function LevelIcon(props: { level: FindingLevel }): ReactElement {
  switch (props.level) {
    case 'blocking':
      return (
        <CrossCircledIcon className="mt-0.5 h-4 w-4 shrink-0 text-danger-fg" />
      );
    case 'warning':
      return (
        <ExclamationTriangleIcon className="mt-0.5 h-4 w-4 shrink-0 text-warn-fg" />
      );
    default:
      return <InfoCircledIcon className="mt-0.5 h-4 w-4 shrink-0 text-link" />;
  }
}

function levelTextClass(level: FindingLevel): string {
  switch (level) {
    case 'blocking':
      return 'text-danger-fg';
    case 'warning':
      return 'text-warn-fg';
    default:
      return 'text-link';
  }
}
