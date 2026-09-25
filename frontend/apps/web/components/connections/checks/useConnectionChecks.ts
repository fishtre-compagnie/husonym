import { getErrorMessage } from '@/util/util';
import { useMutation } from '@connectrpc/connect-query';
import {
  ConnectionCheck,
  ConnectionCheck_Level,
  ConnectionService,
} from '@husonym/sdk';
import { useCallback } from 'react';
import { ConnectionCheckTarget, splitScope } from './targets';

// What a connection cannot do that its role needs, and why it could not be asked, if it
// could not.
export interface ConnectionCheckResult {
  target: ConnectionCheckTarget;
  // What was found, including in the slices of tables asked before one failed.
  checks: ConnectionCheck[];
  // Set when the API could not ask the connection, or a slice of its tables: it may still be
  // reachable from the worker, so this is reported as a warning. It never hides what was
  // found before it.
  unreachable?: string;
}

// useConnectionChecks asks the API what each connection cannot do in its role, the way a
// run checks it at its start.
export function useConnectionChecks(): (
  targets: ConnectionCheckTarget[]
) => Promise<ConnectionCheckResult[]> {
  const { mutateAsync: checkConnectionConfigById } = useMutation(
    ConnectionService.method.checkConnectionConfigById
  );

  return useCallback(
    (targets) =>
      Promise.all(
        targets.map(async (target): Promise<ConnectionCheckResult> => {
          const checks: ConnectionCheck[] = [];
          const seen = new Set<string>();
          try {
            // One call per slice of tables, one after the other: each call holds a
            // connection to the database for as long as it asks.
            for (const scope of splitScope(target.scope)) {
              const res = await checkConnectionConfigById({
                id: target.connectionId,
                scope,
              });
              if (!res.isConnected) {
                return {
                  target,
                  checks,
                  unreachable: res.connectionError ?? 'unable to connect',
                };
              }
              // What concerns the server as a whole comes back with every slice.
              for (const check of res.checks) {
                const key = `${check.kind}|${check.table}|${check.message}`;
                if (!seen.has(key)) {
                  seen.add(key);
                  checks.push(check);
                }
              }
            }
          } catch (err) {
            return { target, checks, unreachable: getErrorMessage(err) };
          }
          return { target, checks };
        })
      ),
    [checkConnectionConfigById]
  );
}

// isBlocking tells whether a finding stops the run. A level this page does not know is
// not taken for blocking: it would keep a job from being saved for nothing it can tell.
export function isBlocking(check: ConnectionCheck): boolean {
  return check.level === ConnectionCheck_Level.BLOCKING;
}

export interface FindingCounts {
  blocking: number;
  warnings: number;
}

export function countChecks(checks: ConnectionCheck[]): FindingCounts {
  const blocking = checks.filter(isBlocking).length;
  return { blocking, warnings: checks.length - blocking };
}

// countFindings counts the findings of every connection; one that could not be asked
// counts as a warning.
export function countFindings(results: ConnectionCheckResult[]): FindingCounts {
  let blocking = 0;
  let warnings = 0;
  for (const result of results) {
    const counts = countChecks(result.checks);
    blocking += counts.blocking;
    warnings += counts.warnings + (result.unreachable ? 1 : 0);
  }
  return { blocking, warnings };
}
