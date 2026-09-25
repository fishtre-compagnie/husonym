'use client';
import ButtonText from '@/components/ButtonText';
import Spinner from '@/components/Spinner';
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert';
import { Button } from '@/components/ui/button';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import { Job } from '@husonym/sdk';
import { ReactElement, useRef, useState } from 'react';
import PreflightReportView from './PreflightReportView';
import { countReport, summarize } from './report';
import { useJobConnections } from './useJobConnections';
import { usePreflightCheck } from './usePreflightCheck';

interface PreflightThenRun {
  // start checks what the run would meet, then starts it when nothing stops it nor warns;
  // otherwise the dialog tells what was found, and the person decides.
  start(): Promise<void>;
  // The dialog, to render in the page.
  dialog: ReactElement;
}

// usePreflightThenRun puts the pre-flight check before a run started by hand. The check
// takes seconds: the dialog opens at once, and says what it waits for. A blocking finding
// leaves only Close: the run would stop at its start on it anyway. A check that did not end
// leaves the choice: the run checks again at its start.
export function usePreflightThenRun(
  job: Job | undefined,
  run: () => Promise<void>
): PreflightThenRun {
  const { check, isChecking, outcome } = usePreflightCheck(job?.id ?? '');
  const connections = useJobConnections(job, job?.accountId);
  const [open, setOpen] = useState(false);
  const [isRunning, setIsRunning] = useState(false);
  // The start() whose check is waited for, none when no check is: closing the dialog
  // meanwhile means the run is not wanted any more. Each start() has its own, so that one
  // left behind never releases the one that followed it.
  const waiting = useRef(0);
  const starts = useRef(0);

  async function start(): Promise<void> {
    if (waiting.current !== 0 || !job) {
      return;
    }
    const current = ++starts.current;
    waiting.current = current;
    setOpen(true);
    try {
      const found = await check();
      if (!found || waiting.current !== current) {
        return;
      }
      if (found.report) {
        const counts = countReport(found.report);
        if (counts.blocking === 0 && counts.warnings === 0) {
          setOpen(false);
          await run();
        }
      }
    } finally {
      if (waiting.current === current) {
        waiting.current = 0;
      }
    }
  }

  function onOpenChange(next: boolean): void {
    if (!next) {
      waiting.current = 0;
    }
    setOpen(next);
  }

  async function runAnyway(): Promise<void> {
    setIsRunning(true);
    try {
      await run();
      setOpen(false);
    } finally {
      setIsRunning(false);
    }
  }

  const report = outcome?.report;
  const counts = report ? countReport(report) : undefined;
  const blocked = !isChecking && !!counts && counts.blocking > 0;

  return {
    start,
    dialog: (
      <Dialog open={open} onOpenChange={onOpenChange}>
        <DialogContent className="max-w-3xl flex flex-col gap-4">
          <DialogHeader>
            <DialogTitle>Pre-flight</DialogTitle>
            <DialogDescription>
              {isChecking
                ? 'Checking what the run will meet before starting it…'
                : counts
                  ? summarize(counts)
                  : 'The check did not end.'}
            </DialogDescription>
          </DialogHeader>
          {isChecking ? (
            <div className="flex flex-row items-center gap-3 rounded-lg border p-4 text-sm text-muted-foreground">
              <Spinner />
              <span>
                A worker computes the plan of the run and asks the connections
                of the job. It takes a few seconds, up to a minute on a large
                schema. Nothing is read from the tables nor written.
              </span>
            </div>
          ) : null}
          {!isChecking && outcome?.error ? (
            <Alert variant="destructive">
              <AlertTitle>The check did not end</AlertTitle>
              <AlertDescription>
                {outcome.error} The run checks again at its start.
              </AlertDescription>
            </Alert>
          ) : null}
          {!isChecking && report ? (
            <div className="flex max-h-[60vh] flex-col overflow-y-auto pr-1">
              <PreflightReportView
                report={report}
                connections={connections}
                copyableRemedies
              />
            </div>
          ) : null}
          <DialogFooter className="gap-2">
            {!isChecking && report ? (
              <Button
                type="button"
                variant="outline"
                disabled={isRunning}
                onClick={() => void check()}
              >
                Check again
              </Button>
            ) : null}
            {blocked ? (
              <Button type="button" onClick={() => onOpenChange(false)}>
                Close
              </Button>
            ) : (
              <>
                <Button
                  type="button"
                  variant="outline"
                  disabled={isRunning}
                  onClick={() => onOpenChange(false)}
                >
                  Cancel
                </Button>
                {!isChecking ? (
                  <Button
                    type="button"
                    disabled={isRunning}
                    onClick={() => void runAnyway()}
                  >
                    <ButtonText
                      leftIcon={isRunning ? <Spinner /> : undefined}
                      text={
                        counts && counts.warnings === 0 && !outcome?.error
                          ? 'Run'
                          : 'Run anyway'
                      }
                    />
                  </Button>
                ) : null}
              </>
            )}
          </DialogFooter>
        </DialogContent>
      </Dialog>
    ),
  };
}
