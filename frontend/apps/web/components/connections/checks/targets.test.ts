import { create } from '@bufbuild/protobuf';
import {
  ConnectionCheckScopeSchema,
  ConnectionCheckTableSchema,
  ConnectionRole,
  JobDestinationOptionsSchema,
  JobEngine,
  JobMappingTransformerSchema,
  JobSourceSchema,
} from '@husonym/sdk';
import {
  getCheckTargetsOfJob,
  getUserDefinedTransformerIds,
  splitScope,
} from './targets';

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

  it('checks a destination on the columns the run writes into it', () => {
    const generateDefault = create(JobMappingTransformerSchema, {
      config: { config: { case: 'generateDefaultConfig', value: {} } },
    });
    const columnsOf = (engine: JobEngine) => {
      const [source, destination] = getCheckTargetsOfJob({
        source: mysqlSource('src'),
        destinations: [
          { connectionId: 'dst', options: postgresDestination(false, false) },
        ],
        mappings: [
          { schema: 'public', table: 'users', column: 'id' },
          {
            schema: 'public',
            table: 'users',
            column: 'created_at',
            transformer: generateDefault,
          },
        ],
        workflowOptions: { engine },
      });
      return [
        source.scope.tables[0].columns,
        destination.scope.tables[0].columns,
      ];
    };

    // Benthos writes DEFAULT into the column: the destination must have it.
    expect(columnsOf(JobEngine.BENTHOS)).toEqual([
      ['id', 'created_at'],
      ['id', 'created_at'],
    ]);
    // Athanor leaves it out of its INSERT; so does the page when the engine is the
    // deployment's own, and the run checks it at its start. The source is read as is.
    for (const engine of [JobEngine.ATHANOR, JobEngine.UNSPECIFIED]) {
      expect(columnsOf(engine)).toEqual([['id', 'created_at'], ['id']]);
    }
  });

  it('takes a user defined transformer made from Generate Default as one', () => {
    const udt = (id: string) =>
      create(JobMappingTransformerSchema, {
        config: {
          config: { case: 'userDefinedTransformerConfig', value: { id } },
        },
      });
    const mappings = [
      {
        schema: 'public',
        table: 'users',
        column: 'id',
        transformer: udt('other'),
      },
      {
        schema: 'public',
        table: 'users',
        column: 'created_at',
        transformer: udt('default'),
      },
    ];
    expect(getUserDefinedTransformerIds(mappings)).toEqual([
      'other',
      'default',
    ]);

    const [, destination] = getCheckTargetsOfJob(
      {
        source: mysqlSource('src'),
        destinations: [
          { connectionId: 'dst', options: postgresDestination(false, false) },
        ],
        mappings,
        workflowOptions: { engine: JobEngine.ATHANOR },
      },
      { defaultTransformerIds: new Set(['default']) }
    );
    expect(destination.scope.tables[0].columns).toEqual(['id']);
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

  it('checks only the servers of an AI generate job, as its run names no column', () => {
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
    expect(target.scope.tables).toEqual([]);
  });

  it('leaves out the mappings of columns the source no longer has', () => {
    const [source, destination] = getCheckTargetsOfJob(
      {
        source: mysqlSource('src'),
        destinations: [
          { connectionId: 'dst', options: postgresDestination(false, false) },
        ],
        mappings: [
          { schema: 'public', table: 'users', column: 'id' },
          { schema: 'public', table: 'users', column: 'dropped' },
          { schema: 'public', table: 'gone', column: 'id' },
        ],
      },
      { sourceColumns: new Set(['public.users.id', 'public.users.email']) }
    );
    for (const target of [source, destination]) {
      expect(
        target.scope.tables.map((t) => [t.schema, t.table, t.columns])
      ).toEqual([['public', 'users', ['id']]]);
    }
  });

  it('leaves the source out, or the tables, when asked', () => {
    const job = {
      source: mysqlSource('src'),
      destinations: [
        { connectionId: 'dst', options: postgresDestination(false, false) },
      ],
      mappings: [{ schema: 'public', table: 'users', column: 'id' }],
    };
    const targets = getCheckTargetsOfJob(job, {
      checkSource: false,
      serverOnly: true,
    });
    expect(targets.map((t) => t.connectionId)).toEqual(['dst']);
    expect(targets[0].scope.tables).toEqual([]);
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
