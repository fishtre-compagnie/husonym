import RunTimeline from '@/components/RunTImeline/RunTimeline';
import { Panel, PanelHeader, Segmented } from '@/components/ds';
import Spinner from '@/components/Spinner';
import SkeletonTable from '@/components/skeleton/SkeletonTable';
import { JobRunEvent, JobRunStatus as JobRunStatusEnum } from '@husonym/sdk';
import { ReactElement, useMemo, useState } from 'react';
import { getColumns } from './JobRunActivityTable/columns';
import { DataTable } from './JobRunActivityTable/data-table';

interface JobRunActivityTableProps {
  jobRunEvents?: JobRunEvent[];
  onViewSelectClicked(schema: string, table: string): void;
  jobStatus?: JobRunStatusEnum;
  isRefreshing?: boolean;
}

type ActivityView = 'timeline' | 'table';

/** Every activity of the run, on a time axis or as a table. */
export default function JobRunActivityTable(
  props: JobRunActivityTableProps
): ReactElement {
  const { jobRunEvents, onViewSelectClicked, jobStatus, isRefreshing } = props;
  const [view, setView] = useState<ActivityView>('timeline');

  const columns = useMemo(() => getColumns({ onViewSelectClicked }), []);

  const isError =
    jobRunEvents?.some((e) => e.tasks.some((t) => t.error)) ?? false;

  return (
    <Panel aria-label="Activity">
      <PanelHeader
        title="Activity"
        actions={
          <>
            {isRefreshing && <Spinner className="h-4 w-4" />}
            <Segmented<ActivityView>
              ariaLabel="Show the activity as"
              value={view}
              onChange={setView}
              options={[
                { value: 'timeline', label: 'Timeline' },
                { value: 'table', label: 'Table' },
              ]}
            />
          </>
        }
      />
      {!jobRunEvents ? (
        <div className="p-5">
          <SkeletonTable />
        </div>
      ) : view === 'timeline' ? (
        <RunTimeline jobRunEvents={jobRunEvents} jobStatus={jobStatus} />
      ) : (
        <DataTable
          columns={columns}
          data={jobRunEvents}
          isError={isError}
          onViewSelectClicked={onViewSelectClicked}
        />
      )}
    </Panel>
  );
}
