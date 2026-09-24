'use client';
import { getErrorMessage } from '@/util/util';
import { ReactElement, useRef, useState } from 'react';
import { toast } from 'sonner';
import ConnectionChecksDialog from './ConnectionChecksDialog';
import { ConnectionCheckTarget } from './targets';
import {
  ConnectionCheckResult,
  countFindings,
  useConnectionChecks,
} from './useConnectionChecks';

interface CheckedSave {
  // checkThenSave checks the connections of the job, then saves it when nothing is found;
  // otherwise it opens the dialog, from which the job is saved unless a finding blocks it.
  checkThenSave(
    targets: ConnectionCheckTarget[],
    save: () => Promise<void>
  ): Promise<void>;
  isChecking: boolean;
  // The dialog, to render in the page.
  dialog: ReactElement;
}

// useCheckedSave keeps a job from being saved while one of its connections lacks what its
// role needs, as the run would find at its start.
export function useCheckedSave(): CheckedSave {
  const check = useConnectionChecks();
  const [open, setOpen] = useState(false);
  const [results, setResults] = useState<ConnectionCheckResult[]>([]);
  const [isChecking, setIsChecking] = useState(false);
  const [isSaving, setIsSaving] = useState(false);
  const pending = useRef<{
    targets: ConnectionCheckTarget[];
    save: () => Promise<void>;
  }>(undefined);

  async function run(
    targets: ConnectionCheckTarget[]
  ): Promise<ConnectionCheckResult[]> {
    setIsChecking(true);
    try {
      const found = await check(targets);
      setResults(found);
      return found;
    } finally {
      setIsChecking(false);
    }
  }

  async function checkThenSave(
    targets: ConnectionCheckTarget[],
    save: () => Promise<void>
  ): Promise<void> {
    pending.current = { targets, save };
    const found = await run(targets);
    const { blocking, warnings } = countFindings(found);
    if (blocking === 0 && warnings === 0) {
      await save();
      return;
    }
    setOpen(true);
  }

  async function onCheckAgain(): Promise<void> {
    if (pending.current) {
      await run(pending.current.targets);
    }
  }

  async function onSave(): Promise<void> {
    const current = pending.current;
    // A blocking finding may only be shown with a Close button: the save is refused here
    // too, so that no path around the dialog saves the job.
    if (!current || countFindings(results).blocking > 0) {
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
    isChecking,
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
