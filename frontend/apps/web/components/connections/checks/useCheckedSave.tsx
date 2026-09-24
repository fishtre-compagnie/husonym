'use client';
import { getErrorMessage } from '@/util/util';
import { useMutation } from '@connectrpc/connect-query';
import { ConnectionDataService } from '@husonym/sdk';
import { ReactElement, useRef, useState } from 'react';
import { toast } from 'sonner';
import ConnectionChecksDialog from './ConnectionChecksDialog';
import {
  CheckedJob,
  ConnectionCheckTarget,
  getCheckTargetsOfJob,
  getSqlSourceConnectionId,
  JobCheckOptions,
} from './targets';
import {
  ConnectionCheckResult,
  countFindings,
  useConnectionChecks,
} from './useConnectionChecks';

interface CheckedSave {
  // checkThenSave checks the connections of the job, then saves it when nothing is found;
  // otherwise it opens the dialog, from which the job is saved unless a finding blocks it.
  checkThenSave(
    job: CheckedJob,
    save: () => Promise<void>,
    options?: Omit<JobCheckOptions, 'sourceColumns'>
  ): Promise<void>;
  // The dialog, to render in the page.
  dialog: ReactElement;
}

interface PendingSave {
  job: CheckedJob;
  options?: Omit<JobCheckOptions, 'sourceColumns'>;
  save: () => Promise<void>;
}

// useCheckedSave keeps a job from being saved, from these pages, while one of its connections
// lacks what its role needs, as the run would find at its start. It is a help to whoever
// configures the job, not a control: the API saves such a job when asked directly, and a
// connection the API cannot ask is let through. The run's own check at its start is what
// stops it.
export function useCheckedSave(): CheckedSave {
  const check = useConnectionChecks();
  const { mutateAsync: getSchemaMap } = useMutation(
    ConnectionDataService.method.getConnectionSchemaMap
  );
  const [open, setOpen] = useState(false);
  const [results, setResults] = useState<ConnectionCheckResult[]>([]);
  const [isChecking, setIsChecking] = useState(false);
  const [isSaving, setIsSaving] = useState(false);
  const pending = useRef<PendingSave>(undefined);
  // Set from a submit until its check, and its save when nothing was found, are over: a
  // second submit meanwhile would ask the databases again, and save the job twice.
  const inFlight = useRef(false);
  // The last check asked for: an earlier one that ends after it is not shown.
  const lastCheck = useRef(0);

  // getTargets reads the columns the source has, so that the mappings of columns it no
  // longer has are left out as a run leaves them out. Unreadable, the mappings are taken as
  // they are: the source's own check then says it cannot be reached.
  async function getTargets(
    job: CheckedJob,
    options?: Omit<JobCheckOptions, 'sourceColumns'>
  ): Promise<ConnectionCheckTarget[]> {
    const sourceId = getSqlSourceConnectionId(job.source);
    let sourceColumns: Set<string> | undefined;
    if (sourceId && !options?.serverOnly && job.mappings.length > 0) {
      try {
        const res = await getSchemaMap({ connectionId: sourceId });
        sourceColumns = new Set(
          Object.values(res.schemaMap).flatMap((table) =>
            table.schemas.map((c) => `${c.schema}.${c.table}.${c.column}`)
          )
        );
      } catch {
        sourceColumns = undefined;
      }
    }
    return getCheckTargetsOfJob(job, { ...options, sourceColumns });
  }

  // run checks the pending job, and tells whether this check is still the last one asked.
  async function run(
    current: PendingSave
  ): Promise<ConnectionCheckResult[] | undefined> {
    const id = ++lastCheck.current;
    setIsChecking(true);
    try {
      const found = await check(await getTargets(current.job, current.options));
      if (id !== lastCheck.current) {
        return undefined;
      }
      setResults(found);
      return found;
    } finally {
      if (id === lastCheck.current) {
        setIsChecking(false);
      }
    }
  }

  async function checkThenSave(
    job: CheckedJob,
    save: () => Promise<void>,
    options?: Omit<JobCheckOptions, 'sourceColumns'>
  ): Promise<void> {
    if (inFlight.current) {
      return;
    }
    inFlight.current = true;
    const checking = toast.loading('Checking the connections of the job…');
    try {
      const current = { job, options, save };
      pending.current = current;
      const found = await run(current);
      toast.dismiss(checking);
      if (!found) {
        return;
      }
      const { blocking, warnings } = countFindings(found);
      if (blocking === 0 && warnings === 0) {
        await save();
        return;
      }
      setOpen(true);
    } finally {
      toast.dismiss(checking);
      inFlight.current = false;
    }
  }

  async function onCheckAgain(): Promise<void> {
    if (pending.current) {
      await run(pending.current);
    }
  }

  async function onSave(): Promise<void> {
    const current = pending.current;
    // A blocking finding may only be shown with a Close button: the save is refused here
    // too, so that no path around the dialog saves the job.
    if (!current || isChecking || countFindings(results).blocking > 0) {
      return;
    }
    setIsSaving(true);
    try {
      await current.save();
      setOpen(false);
    } catch (err) {
      toast.error('Unable to save the job', {
        description: getErrorMessage(err),
      });
    } finally {
      setIsSaving(false);
    }
  }

  return {
    checkThenSave,
    dialog: (
      <ConnectionChecksDialog
        open={open}
        onOpenChange={setOpen}
        results={results}
        isChecking={isChecking}
        isSaving={isSaving}
        onCheckAgain={onCheckAgain}
        onSave={onSave}
      />
    ),
  };
}
