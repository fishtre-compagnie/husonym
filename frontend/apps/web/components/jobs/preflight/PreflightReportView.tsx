'use client';
import FindingList from '@/components/findings/FindingList';
import { PreflightReport } from '@husonym/sdk';
import { CheckCircledIcon } from '@radix-ui/react-icons';
import { ReactElement } from 'react';
import { groupReport, JobConnection } from './report';

interface Props {
  report: PreflightReport;
  connections: JobConnection[];
}

// PreflightReportView shows a pre-flight report: what the plan of the run tells, then what
// each connection of the job tells.
export default function PreflightReportView(props: Props): ReactElement {
  const groups = groupReport(props.report, props.connections);
  if (groups.length === 0) {
    return (
      <div className="flex flex-row items-center gap-2 text-sm text-green-800 dark:text-green-400">
        <CheckCircledIcon className="h-4 w-4 shrink-0" />
        <span>Nothing to report.</span>
      </div>
    );
  }
  return (
    <div className="flex flex-col gap-5">
      {groups.map((group) => (
        <div key={group.key} className="flex flex-col gap-2">
          <h3 className="text-sm font-semibold">{group.title}</h3>
          <FindingList findings={group.findings} />
        </div>
      ))}
    </div>
  );
}
