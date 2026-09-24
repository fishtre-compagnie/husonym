import { create } from '@bufbuild/protobuf';
import {
  ConnectionCheckScope,
  ConnectionCheckScopeSchema,
  ConnectionCheckTable,
  ConnectionCheckTableSchema,
  ConnectionRole,
  JobDestinationOptions,
  JobEngine,
  JobMapping,
  JobSource,
} from '@husonym/sdk';

// The API checks at most this many tables in one call.
const MAX_CHECKED_TABLES = 1000;

// A connection of a job, the role it plays there, and what the job asks of it: one or more
// calls to CheckConnectionConfigById.
export interface ConnectionCheckTarget {
  connectionId: string;
  role: ConnectionRole;
  scope: ConnectionCheckScope;
}

// The parts of a job, saved or about to be, that decide what it asks of its connections.
export type CheckedJob = {
  source?: JobSource;
  destinations: { connectionId: string; options?: JobDestinationOptions }[];
  mappings: Pick<JobMapping, 'schema' | 'table' | 'column'>[];
  workflowOptions?: { engine: JobEngine };
};

export interface JobCheckOptions {
  // The source is checked too; a screen that changes only destinations leaves it out.
  checkSource?: boolean;
  // Only what concerns the servers as a whole is checked: what the engine changes.
  serverOnly?: boolean;
  // The columns the source has, as schema.table.column: a mapping whose column the source no
  // longer has is left out, as a run leaves it out.
  sourceColumns?: Set<string>;
}

// getCheckTargetsOfJob gives the MySQL and PostgreSQL connections of a job to check, each in
// its role, as the run checks them at its start, on the tables and columns of its mappings.
// Other connections are not checked.
//
// An AI generate job has no mappings: the columns it writes are those the model returns, and
// its run checks its table without naming them. Only its servers are checked here, so that
// the page never asks more of the table than the run does.
export function getCheckTargetsOfJob(
  job: CheckedJob,
  options: JobCheckOptions = {}
): ConnectionCheckTarget[] {
  const engine = job.workflowOptions?.engine ?? JobEngine.UNSPECIFIED;
  const tables = options.serverOnly
    ? []
    : toCheckedTables(job.mappings, options.sourceColumns);
  const targets: ConnectionCheckTarget[] = [];
  const sourceId = getSqlSourceConnectionId(job.source);
  if (sourceId && options.checkSource !== false) {
    targets.push({
      connectionId: sourceId,
      role: ConnectionRole.SOURCE,
      scope: create(ConnectionCheckScopeSchema, {
        role: ConnectionRole.SOURCE,
        engine,
        tables,
      }),
    });
  }
  for (const destination of job.destinations) {
    const config = destination.options?.config;
    if (
      !destination.connectionId ||
      (config?.case !== 'mysqlOptions' && config?.case !== 'postgresOptions')
    ) {
      continue;
    }
    targets.push({
      connectionId: destination.connectionId,
      role: ConnectionRole.DESTINATION,
      scope: create(ConnectionCheckScopeSchema, {
        role: ConnectionRole.DESTINATION,
        engine,
        tables,
        initTableSchema: config.value.initTableSchema,
        truncateBeforeInsert:
          config.value.truncateTable?.truncateBeforeInsert ?? false,
      }),
    });
  }
  return targets;
}

// getServerScope asks what a connection cannot do in a role before the job has tables:
// only what concerns the server as a whole is checked.
export function getServerScope(
  role: ConnectionRole,
  engine?: JobEngine
): ConnectionCheckScope {
  return create(ConnectionCheckScopeSchema, {
    role,
    engine: engine ?? JobEngine.UNSPECIFIED,
  });
}

// getSqlSourceConnectionId is the source connection a run reads, when it is MySQL or
// PostgreSQL.
export function getSqlSourceConnectionId(
  source?: JobSource
): string | undefined {
  const config = source?.options?.config;
  if (config?.case === 'mysql' || config?.case === 'postgres') {
    return config.value.connectionId || undefined;
  }
  return undefined;
}

// toCheckedTables groups the mappings of a job by table, with the columns each maps, leaving
// out those the source does not have when its columns are known.
function toCheckedTables(
  mappings: Pick<JobMapping, 'schema' | 'table' | 'column'>[],
  sourceColumns?: Set<string>
): ConnectionCheckTable[] {
  const byName = new Map<string, ConnectionCheckTable>();
  for (const mapping of mappings) {
    if (!mapping.schema || !mapping.table || !mapping.column) {
      continue;
    }
    const key = `${mapping.schema}.${mapping.table}`;
    if (sourceColumns && !sourceColumns.has(`${key}.${mapping.column}`)) {
      continue;
    }
    let table = byName.get(key);
    if (!table) {
      table = create(ConnectionCheckTableSchema, {
        schema: mapping.schema,
        table: mapping.table,
      });
      byName.set(key, table);
    }
    if (!table.columns.includes(mapping.column)) {
      table.columns.push(mapping.column);
    }
  }
  return [...byName.values()];
}

// splitScope cuts a scope into scopes of at most MAX_CHECKED_TABLES tables each; a scope
// without tables stays whole.
export function splitScope(
  scope: ConnectionCheckScope
): ConnectionCheckScope[] {
  if (scope.tables.length <= MAX_CHECKED_TABLES) {
    return [scope];
  }
  const scopes: ConnectionCheckScope[] = [];
  for (let i = 0; i < scope.tables.length; i += MAX_CHECKED_TABLES) {
    scopes.push(
      create(ConnectionCheckScopeSchema, {
        ...scope,
        tables: scope.tables.slice(i, i + MAX_CHECKED_TABLES),
      })
    );
  }
  return scopes;
}
