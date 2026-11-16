'use client';
import { getErrorMessage } from '@/util/util';
import { timestampDate } from '@bufbuild/protobuf/wkt';
import { useMutation } from '@connectrpc/connect-query';
import { Code, ConnectError, JobService, PreflightReport } from '@husonym/sdk';
import { useTranslations } from 'next-intl';
import { useRef, useState } from 'react';

interface PreflightOutcome {
  report?: PreflightReport;
  checkedAt?: Date;
  // Why the check did not end, when it did not.
  error?: string;
}

interface PreflightCheck {
  // check asks the pre-flight check of the job, and returns what it found; undefined when a
  // later check was asked meanwhile.
  check(): Promise<PreflightOutcome | undefined>;
  isChecking: boolean;
  outcome?: PreflightOutcome;
}

// usePreflightCheck asks what a run of a job would meet, before any run: the plan is
// computed by a worker and the connections are asked, nothing is read nor written. It takes
// seconds, up to a minute on a large schema.
export function usePreflightCheck(jobId: string): PreflightCheck {
  const t = useTranslations('mapping.preflight');
  const { mutateAsync } = useMutation(JobService.method.preflightJob);
  const [isChecking, setIsChecking] = useState(false);
  const [outcome, setOutcome] = useState<PreflightOutcome>();
  const lastCheck = useRef(0);

  async function check(): Promise<PreflightOutcome | undefined> {
    const id = ++lastCheck.current;
    setIsChecking(true);
    let found: PreflightOutcome;
    try {
      const resp = await mutateAsync({ jobId });
      found = resp.report
        ? {
            report: resp.report,
            checkedAt: resp.checkedAt
              ? timestampDate(resp.checkedAt)
              : new Date(),
          }
        : { error: t('noReport') };
    } catch (err) {
      found = { error: preflightErrorMessage(err, t('permissionDenied')) };
    }
    if (id !== lastCheck.current) {
      return undefined;
    }
    setOutcome(found);
    setIsChecking(false);
    return found;
  }

  return { check, isChecking, outcome };
}

// preflightErrorMessage says why a check did not end, in the words of the page.
function preflightErrorMessage(err: unknown, permissionDenied: string): string {
  if (err instanceof ConnectError && err.code === Code.PermissionDenied) {
    return permissionDenied;
  }
  return getErrorMessage(err);
}
