import { create } from '@bufbuild/protobuf';
import {
  ConnectionCheckScopeSchema,
  ConnectionCheckTableSchema,
  ConnectionRole,
  JobDestinationOptionsSchema,
  JobEngine,
  JobSourceSchema,
} from '@husonym/sdk';
import { getCheckTargetsOfJob, splitScope } from './targets';

function mysqlSource(connectionId: string) {
  return create(JobSourceSchema, {
    options: { config: { case: 'mysql', value: { connectionId } } },
  });
}

function postgresDestination(truncate: boolean, init: boolean) {
  return create(JobDestinationOptionsSchema, {
    config: {
      case: 'postgresOptions',
      value: {
        initTableSchema: init,
        truncateTable: { truncateBeforeInsert: truncate },
      },
    },
  });
}

describe('getCheckTargetsOfJob', () => {
  it('checks the source and each SQL destination on the tables of the mappings', () => {
    const targets = getCheckTargetsOfJob({
      source: mysqlSource('src'),
      destinations: [
        { connectionId: 'dst', options: postgresDestination(true, false) },
        {
          connectionId: 's3',
          options: create(JobDestinationOptionsSchema, {
            config: { case: 'awsS3Options', value: {} },
          }),
        },
      ],
      mappings: [
        { schema: 'public', table: 'users', column: 'id' },
        { schema: 'public', table: 'users', column: 'email' },
        { schema: 'public', table: 'users', column: 'email' },
        { schema: 'public', table: 'orders', column: 'id' },
      ],
      workflowOptions: { engine: JobEngine.ATHANOR },
    });

    expect(targets.map((t) => [t.connectionId, t.role])).toEqual([
      ['src', ConnectionRole.SOURCE],
      ['dst', ConnectionRole.DESTINATION],
    ]);
    const [source, destination] = targets;
    expect(
      source.scope.tables.map((t) => [t.schema, t.table, t.columns])
    ).toEqual([
      ['public', 'users', ['id', 'email']],
      ['public', 'orders', ['id']],
    ]);
    expect(source.scope.engine).toBe(JobEngine.ATHANOR);
    expect(source.scope.truncateBeforeInsert).toBe(false);
    expect(destination.scope.truncateBeforeInsert).toBe(true);
    expect(destination.scope.initTableSchema).toBe(false);
    expect(destination.scope.engine).toBe(JobEngine.ATHANOR);
  });

  it('leaves the engine unspecified when the job does not choose one', () => {
    const [target] = getCheckTargetsOfJob({
      destinations: [
        { connectionId: 'dst', options: postgresDestination(false, true) },
      ],
      mappings: [],
    });
    expect(target.scope.engine).toBe(JobEngine.UNSPECIFIED);
    expect(target.scope.initTableSchema).toBe(true);
    expect(target.scope.tables).toEqual([]);
  });

  it('checks the table an AI generate job fills on all of its columns', () => {
    const [target] = getCheckTargetsOfJob({
      source: create(JobSourceSchema, {
        options: {
          config: {
            case: 'aiGenerate',
            value: {
              aiConnectionId: 'ai',
              schemas: [{ schema: 'public', tables: [{ table: 'users' }] }],
            },
          },
        },
      }),
      destinations: [
        { connectionId: 'dst', options: postgresDestination(false, false) },
      ],
      mappings: [],
    });
    // the AI connection is not a database: only the destination is checked
    expect(target.role).toBe(ConnectionRole.DESTINATION);
    expect(
      target.scope.tables.map((t) => [t.schema, t.table, t.columns])
    ).toEqual([['public', 'users', []]]);
  });

  it('checks nothing for a job without SQL connections', () => {
    expect(
      getCheckTargetsOfJob({
        source: create(JobSourceSchema, {
          options: { config: { case: 'generate', value: {} } },
        }),
        destinations: [{ connectionId: '', options: undefined }],
        mappings: [{ schema: 'public', table: 'users', column: 'id' }],
      })
    ).toEqual([]);
  });
});

describe('splitScope', () => {
  function scopeWith(count: number) {
    return create(ConnectionCheckScopeSchema, {
      role: ConnectionRole.DESTINATION,
      truncateBeforeInsert: true,
      tables: Array.from({ length: count }, (_, i) =>
        create(ConnectionCheckTableSchema, { schema: 's', table: `t${i}` })
      ),
    });
  }

  it('keeps a scope of at most 1000 tables whole', () => {
    const scope = scopeWith(1000);
    expect(splitScope(scope)).toEqual([scope]);
    expect(splitScope(scopeWith(0))).toHaveLength(1);
  });

  it('cuts a larger scope into slices of 1000 tables, keeping its options', () => {
    const slices = splitScope(scopeWith(2001));
    expect(slices.map((s) => s.tables.length)).toEqual([1000, 1000, 1]);
    expect(slices[2].tables[0].table).toBe('t2000');
    expect(slices.every((s) => s.truncateBeforeInsert)).toBe(true);
    expect(slices.every((s) => s.role === ConnectionRole.DESTINATION)).toBe(
      true
    );
  });
});
