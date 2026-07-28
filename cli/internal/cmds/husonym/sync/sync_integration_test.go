package sync_cmd

import (
	"context"
	"database/sql"
	"testing"

	"connectrpc.com/connect"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	tchusonymapi "github.com/fishtre-compagnie/husonym/backend/pkg/integration-test"
	"github.com/fishtre-compagnie/husonym/cli/internal/output"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	dyntypes "github.com/aws/aws-sdk-go-v2/service/dynamodb/types"

	connectionmanager "github.com/fishtre-compagnie/husonym/internal/connection-manager"
	"github.com/fishtre-compagnie/husonym/internal/testutil"
	tcdynamodb "github.com/fishtre-compagnie/husonym/internal/testutil/testcontainers/dynamodb"
	tcmysql "github.com/fishtre-compagnie/husonym/internal/testutil/testcontainers/mysql"
	tcpostgres "github.com/fishtre-compagnie/husonym/internal/testutil/testcontainers/postgres"
	testutil_testdata "github.com/fishtre-compagnie/husonym/internal/testutil/testdata"
	mysqlalltypes "github.com/fishtre-compagnie/husonym/internal/testutil/testdata/mysql/alltypes"
	pgalltypes "github.com/fishtre-compagnie/husonym/internal/testutil/testdata/postgres/alltypes"

	tcworkflow "github.com/fishtre-compagnie/husonym/internal/integration-tests/worker/workflow"
	"github.com/stretchr/testify/require"
)

const husonymDbMigrationsPath = "../../../../../backend/sql/postgresql/schema"

func Test_Sync(t *testing.T) {
	t.Parallel()
	ok := testutil.ShouldRunCLIIntegrationTest()
	if !ok {
		return
	}
	ctx := context.Background()

	husonymApi, err := tchusonymapi.NewHusonymApiTestClient(
		ctx,
		t,
		tchusonymapi.WithMigrationsDirectory(husonymDbMigrationsPath),
	)
	if err != nil {
		t.Fatal(err)
	}

	connclient := husonymApi.OSSUnauthenticatedLicensedClients.Connections()
	conndataclient := husonymApi.OSSUnauthenticatedLicensedClients.ConnectionData()
	jobclient := husonymApi.OSSUnauthenticatedLicensedClients.Jobs()

	dbManagers := tcworkflow.NewTestDatabaseManagers(t)
	connmanager := dbManagers.SqlConnManager
	sqlmanagerclient := dbManagers.SqlManager
	accountId := tchusonymapi.CreatePersonalAccount(
		ctx,
		t,
		husonymApi.OSSUnauthenticatedLicensedClients.Users(),
	)
	awsS3Config := testutil.GetTestAwsS3Config()
	s3Conn := tchusonymapi.CreateS3Connection(
		ctx,
		t,
		connclient,
		accountId,
		"s3-conn",
		awsS3Config.Bucket,
		&awsS3Config.Region,
	)
	outputType := output.PlainOutput

	t.Run("postgres", func(t *testing.T) {
		t.Parallel()
		postgres, err := tcpostgres.NewPostgresTestSyncContainer(
			ctx,
			[]tcpostgres.Option{},
			[]tcpostgres.Option{},
		)
		if err != nil {
			t.Fatal(err)
		}

		testdataFolder := "../../../../../internal/testutil/testdata/postgres"
		sourceConn := tchusonymapi.CreatePostgresConnection(
			ctx,
			t,
			connclient,
			accountId,
			"postgres-source",
			postgres.Source.URL,
		)

		t.Run("postgres_sync", func(t *testing.T) {
			// can't be run in parallel yet
			// right now CLI sync and init schema takes everything in source and copies it to target since there are no job mappings defined by the user
			// so it can't be scoped to specific schema
			// t.Parallel()
			err = postgres.Source.RunCreateStmtsInSchema(
				ctx,
				testdataFolder,
				[]string{"humanresources/create-tables.sql"},
				"humanresources",
			)
			if err != nil {
				t.Fatal(err)
			}
			alltypesSchema := "alltypes"
			err = postgres.Source.RunCreateStmtsInSchema(
				ctx,
				testdataFolder,
				[]string{"alltypes/create-tables.sql"},
				alltypesSchema,
			)
			if err != nil {
				t.Fatal(err)
			}
			err = postgres.Target.CreateSchemas(ctx, []string{"humanresources", alltypesSchema})
			if err != nil {
				t.Fatal(err)
			}

			testlogger := testutil.GetTestLogger(t)
			cmdconfig := &cmdConfig{
				Source: &sourceConfig{
					ConnectionId: sourceConn.Id,
				},
				Destination: &sqlDestinationConfig{
					ConnectionUrl:        postgres.Target.URL,
					Driver:               postgresDriver,
					InitSchema:           true,
					TruncateBeforeInsert: true,
					TruncateCascade:      true,
				},
				OutputType: &outputType,
				AccountId:  &accountId,
			}
			sync := &clisync{
				connectiondataclient: conndataclient,
				connectionclient:     connclient,
				sqlmanagerclient:     sqlmanagerclient,
				ctx:                  ctx,
				logger:               testlogger,
				cmd:                  cmdconfig,
				connmanager:          connmanager,
				session:              connectionmanager.NewUniqueSession(),
			}
			err := sync.configureAndRunSync()
			require.NoError(t, err)

			rowCount, err := postgres.Target.GetTableRowCount(ctx, "humanresources", "employees")
			require.NoError(t, err)
			require.Greater(t, rowCount, 1)

			rowCount, err = postgres.Target.GetTableRowCount(ctx, "alltypes", "generated_table")
			require.NoError(t, err)
			require.Greater(t, rowCount, 1)

			rowCount, err = postgres.Target.GetTableRowCount(ctx, "alltypes", "all_data_types")
			require.NoError(t, err)
			require.Greater(t, rowCount, 1)

			rowCount, err = postgres.Target.GetTableRowCount(ctx, "alltypes", "time_time")
			require.NoError(t, err)
			require.Greater(t, rowCount, 0)

			source, err := sql.Open("postgres", postgres.Source.URL)
			require.NoError(t, err)
			defer source.Close()

			target, err := sql.Open("postgres", postgres.Target.URL)
			require.NoError(t, err)
			defer target.Close()

			testutil_testdata.VerifySQLTableColumnValues(
				t,
				ctx,
				source,
				target,
				alltypesSchema,
				"all_data_types",
				"postgres",
				[]string{"id"},
			)
			testutil_testdata.VerifySQLTableColumnValues(
				t,
				ctx,
				source,
				target,
				alltypesSchema,
				"time_time",
				"postgres",
				[]string{"id"},
			)
			testutil_testdata.VerifySQLTableColumnValues(
				t,
				ctx,
				source,
				target,
				alltypesSchema,
				"json_data",
				"postgres",
				[]string{"id"},
			)
			testutil_testdata.VerifySQLTableColumnValues(
				t,
				ctx,
				source,
				target,
				alltypesSchema,
				"array_types",
				"postgres",
				[]string{"id"},
			)
			testutil_testdata.VerifySQLTableColumnValues(
				t,
				ctx,
				source,
				target,
				alltypesSchema,
				"generated_table",
				"postgres",
				[]string{"id"},
			)
		})

		t.Run("S3_end_to_end", func(t *testing.T) {
			t.Parallel()
			ok := testutil.ShouldRunS3IntegrationTest()
			if !ok {
				return
			}

			alltypesSchema := "alltypes_s3_pg"
			err := postgres.Source.RunCreateStmtsInSchema(
				ctx,
				testdataFolder,
				[]string{"alltypes/create-tables.sql"},
				alltypesSchema,
			)
			if err != nil {
				t.Fatal(err)
			}

			err = postgres.Target.RunCreateStmtsInSchema(
				ctx,
				testdataFolder,
				[]string{"alltypes/create-tables.sql"},
				alltypesSchema,
			)
			if err != nil {
				t.Fatal(err)
			}

			husonymApi.MockTemporalForCreateJob("cli-test-sync")
			job, err := jobclient.CreateJob(ctx, connect.NewRequest(&mgmtv1alpha1.CreateJobRequest{
				AccountId: accountId,
				JobName:   "S3 to PG",
				Source: &mgmtv1alpha1.JobSource{
					Options: &mgmtv1alpha1.JobSourceOptions{
						Config: &mgmtv1alpha1.JobSourceOptions_Postgres{
							Postgres: &mgmtv1alpha1.PostgresSourceConnectionOptions{
								ConnectionId:                  sourceConn.Id,
								Schemas:                       []*mgmtv1alpha1.PostgresSourceSchemaOption{},
								SubsetByForeignKeyConstraints: true,
							},
						},
					},
				},
				Destinations: []*mgmtv1alpha1.CreateJobDestination{
					{
						ConnectionId: s3Conn.Id,
						Options: &mgmtv1alpha1.JobDestinationOptions{
							Config: &mgmtv1alpha1.JobDestinationOptions_AwsS3Options{
								AwsS3Options: &mgmtv1alpha1.AwsS3DestinationConnectionOptions{},
							},
						},
					},
				},
				Mappings: pgalltypes.GetDefaultSyncJobMappings(alltypesSchema),
			}))
			require.NoError(t, err)

			t.Run("Postgres_to_S3", func(t *testing.T) {
				testworkflow := tcworkflow.NewTestDataSyncWorkflowEnv(t, husonymApi, dbManagers)
				testworkflow.RequireActivitiesCompletedSuccessfully(t)
				testworkflow.ExecuteTestDataSyncWorkflow(job.Msg.GetJob().GetId())
				require.Truef(
					t,
					testworkflow.TestEnv.IsWorkflowCompleted(),
					"Workflow did not complete. Test: pg_to_s3",
				)
				err = testworkflow.TestEnv.GetWorkflowError()
				require.NoError(t, err, "Received Temporal Workflow Error", "testName", "pg_to_s3")
			})

			t.Run("S3_to_Postgres", func(t *testing.T) {
				testlogger := testutil.GetTestLogger(t)
				cmdConfig := &cmdConfig{
					Source: &sourceConfig{
						ConnectionId: s3Conn.Id,
						ConnectionOpts: &connectionOpts{
							JobId: &job.Msg.Job.Id,
						},
					},
					Destination: &sqlDestinationConfig{
						ConnectionUrl:        postgres.Target.URL,
						Driver:               postgresDriver,
						InitSchema:           false,
						TruncateBeforeInsert: true,
						TruncateCascade:      true,
					},
					OutputType: &outputType,
					AccountId:  &accountId,
				}
				sync := &clisync{
					connectiondataclient: conndataclient,
					connectionclient:     connclient,
					sqlmanagerclient:     sqlmanagerclient,
					ctx:                  ctx,
					logger:               testlogger,
					cmd:                  cmdConfig,
					connmanager:          connmanager,
					session:              connectionmanager.NewUniqueSession(),
				}
				err := sync.configureAndRunSync()
				require.NoError(t, err)
				rowCount, err := postgres.Target.GetTableRowCount(
					ctx,
					alltypesSchema,
					"all_data_types",
				)
				require.NoError(t, err)
				require.Greater(t, rowCount, 1)

				rowCount, err = postgres.Target.GetTableRowCount(ctx, alltypesSchema, "json_data")
				require.NoError(t, err)
				require.Greater(t, rowCount, 1)

				rowCount, err = postgres.Target.GetTableRowCount(ctx, alltypesSchema, "time_time")
				require.NoError(t, err)
				require.Greater(t, rowCount, 0)

				source, err := sql.Open("postgres", postgres.Source.URL)
				require.NoError(t, err)
				defer source.Close()

				target, err := sql.Open("postgres", postgres.Target.URL)
				require.NoError(t, err)
				defer target.Close()

				testutil_testdata.VerifySQLTableColumnValues(
					t,
					ctx,
					source,
					target,
					alltypesSchema,
					"all_data_types",
					"postgres",
					[]string{"id"},
				)
				testutil_testdata.VerifySQLTableColumnValues(
					t,
					ctx,
					source,
					target,
					alltypesSchema,
					"time_time",
					"postgres",
					[]string{"id"},
				)
				testutil_testdata.VerifySQLTableColumnValues(
					t,
					ctx,
					source,
					target,
					alltypesSchema,
					"json_data",
					"postgres",
					[]string{"id"},
				)
				testutil_testdata.VerifySQLTableColumnValues(
					t,
					ctx,
					source,
					target,
					alltypesSchema,
					"array_types",
					"postgres",
					[]string{"id"},
				)
			})
		})

		t.Cleanup(func() {
			err := postgres.TearDown(ctx)
			if err != nil {
				t.Fatal(err)
			}
		})
	})

	t.Run("mysql", func(t *testing.T) {
		t.Parallel()
		mysql, err := tcmysql.NewMysqlTestSyncContainer(ctx, []tcmysql.Option{}, []tcmysql.Option{})
		if err != nil {
			t.Fatal(err)
		}

		testdataFolder := "../../../../../internal/testutil/testdata/mysql"
		sourceConn := tchusonymapi.CreateMysqlConnection(
			ctx,
			t,
			connclient,
			accountId,
			"mysql-source",
			mysql.Source.URL,
		)

		t.Run("mysql_sync", func(t *testing.T) {
			// can't be run in parallel yet
			// right now CLI sync and init schema takes everything in source and copies it to target since there are no job mappings defined by the user
			// so it can't be scoped to specific schema
			// t.Parallel()
			alltypesSchema := "alltypes"
			err = mysql.Source.RunCreateStmtsInDatabase(
				ctx,
				testdataFolder,
				[]string{"humanresources/create-tables.sql"},
				"humanresources",
			)
			if err != nil {
				t.Fatal(err)
			}
			err = mysql.Source.RunCreateStmtsInDatabase(
				ctx,
				testdataFolder,
				[]string{"alltypes/create-tables.sql"},
				alltypesSchema,
			)
			if err != nil {
				t.Fatal(err)
			}
			err = mysql.Target.CreateDatabases(ctx, []string{"humanresources", alltypesSchema})
			if err != nil {
				t.Fatal(err)
			}
			testlogger := testutil.GetTestLogger(t)
			cmdConfig := &cmdConfig{
				Source: &sourceConfig{
					ConnectionId: sourceConn.Id,
				},
				Destination: &sqlDestinationConfig{
					ConnectionUrl:        mysql.Target.URL,
					Driver:               mysqlDriver,
					InitSchema:           true,
					TruncateBeforeInsert: true,
				},
				OutputType: &outputType,
				AccountId:  &accountId,
			}
			sync := &clisync{
				connectiondataclient: conndataclient,
				connectionclient:     connclient,
				sqlmanagerclient:     sqlmanagerclient,
				ctx:                  ctx,
				logger:               testlogger,
				cmd:                  cmdConfig,
				connmanager:          connmanager,
				session:              connectionmanager.NewUniqueSession(),
			}
			err := sync.configureAndRunSync()
			require.NoError(t, err)

			rowCount, err := mysql.Target.GetTableRowCount(ctx, "humanresources", "locations")
			require.NoError(t, err)
			require.Greater(t, rowCount, 1)

			rowCount, err = mysql.Target.GetTableRowCount(ctx, alltypesSchema, "generated_table")
			require.NoError(t, err)
			require.Greater(t, rowCount, 1)

			rowCount, err = mysql.Target.GetTableRowCount(ctx, alltypesSchema, "all_data_types")
			require.NoError(t, err)
			require.Greater(t, rowCount, 1)

			testutil_testdata.VerifySQLTableColumnValues(
				t,
				ctx,
				mysql.Source.DB,
				mysql.Target.DB,
				alltypesSchema,
				"json_data",
				"mysql",
				[]string{"id"},
			)
			testutil_testdata.VerifySQLTableColumnValues(
				t,
				ctx,
				mysql.Source.DB,
				mysql.Target.DB,
				alltypesSchema,
				"all_data_types",
				"mysql",
				[]string{"id"},
			)
			testutil_testdata.VerifySQLTableColumnValues(
				t,
				ctx,
				mysql.Source.DB,
				mysql.Target.DB,
				alltypesSchema,
				"generated_table",
				"mysql",
				[]string{"id"},
			)
		})

		t.Run("S3_end_to_end", func(t *testing.T) {
			t.Parallel()
			ok := testutil.ShouldRunS3IntegrationTest()
			if !ok {
				return
			}

			alltypesSchema := "alltypes_s3_mysql"
			err := mysql.Source.RunCreateStmtsInDatabase(
				ctx,
				testdataFolder,
				[]string{"alltypes/create-tables.sql"},
				alltypesSchema,
			)
			if err != nil {
				t.Fatal(err)
			}

			err = mysql.Target.RunCreateStmtsInDatabase(
				ctx,
				testdataFolder,
				[]string{"alltypes/create-tables.sql"},
				alltypesSchema,
			)
			if err != nil {
				t.Fatal(err)
			}

			husonymApi.MockTemporalForCreateJob("cli-test-sync")
			job, err := jobclient.CreateJob(ctx, connect.NewRequest(&mgmtv1alpha1.CreateJobRequest{
				AccountId: accountId,
				JobName:   "S3 to Mysql",
				Source: &mgmtv1alpha1.JobSource{
					Options: &mgmtv1alpha1.JobSourceOptions{
						Config: &mgmtv1alpha1.JobSourceOptions_Mysql{
							Mysql: &mgmtv1alpha1.MysqlSourceConnectionOptions{
								ConnectionId:                  sourceConn.Id,
								Schemas:                       []*mgmtv1alpha1.MysqlSourceSchemaOption{},
								SubsetByForeignKeyConstraints: true,
							},
						},
					},
				},
				Destinations: []*mgmtv1alpha1.CreateJobDestination{
					{
						ConnectionId: s3Conn.Id,
						Options: &mgmtv1alpha1.JobDestinationOptions{
							Config: &mgmtv1alpha1.JobDestinationOptions_AwsS3Options{
								AwsS3Options: &mgmtv1alpha1.AwsS3DestinationConnectionOptions{},
							},
						},
					},
				},
				Mappings: mysqlalltypes.GetDefaultSyncJobMappings(alltypesSchema),
			}))
			require.NoError(t, err)

			t.Run("Mysql_to_S3", func(t *testing.T) {
				testworkflow := tcworkflow.NewTestDataSyncWorkflowEnv(t, husonymApi, dbManagers)
				testworkflow.RequireActivitiesCompletedSuccessfully(t)
				testworkflow.ExecuteTestDataSyncWorkflow(job.Msg.GetJob().GetId())
				require.Truef(
					t,
					testworkflow.TestEnv.IsWorkflowCompleted(),
					"Workflow did not complete. Test: mysql_to_s3",
				)
				err = testworkflow.TestEnv.GetWorkflowError()
				require.NoError(
					t,
					err,
					"Received Temporal Workflow Error",
					"testName",
					"mysql_to_s3",
				)
			})

			t.Run("S3_to_Mysql", func(t *testing.T) {
				testlogger := testutil.GetTestLogger(t)
				cmdConfig := &cmdConfig{
					Source: &sourceConfig{
						ConnectionId: s3Conn.Id,
						ConnectionOpts: &connectionOpts{
							JobId: &job.Msg.Job.Id,
						},
					},
					Destination: &sqlDestinationConfig{
						ConnectionUrl:        mysql.Target.URL,
						Driver:               mysqlDriver,
						InitSchema:           false,
						TruncateBeforeInsert: true,
					},
					OutputType: &outputType,
					AccountId:  &accountId,
				}
				sync := &clisync{
					connectiondataclient: conndataclient,
					connectionclient:     connclient,
					sqlmanagerclient:     sqlmanagerclient,
					ctx:                  ctx,
					logger:               testlogger,
					cmd:                  cmdConfig,
					connmanager:          connmanager,
					session:              connectionmanager.NewUniqueSession(),
				}
				err := sync.configureAndRunSync()
				require.NoError(t, err)
			})

			rowCount, err := mysql.Target.GetTableRowCount(ctx, alltypesSchema, "all_data_types")
			require.NoError(t, err)
			require.Greater(t, rowCount, 1)

			rowCount, err = mysql.Target.GetTableRowCount(ctx, alltypesSchema, "json_data")
			require.NoError(t, err)
			require.Greater(t, rowCount, 1)

			rowCount, err = mysql.Target.GetTableRowCount(ctx, alltypesSchema, "generated_table")
			require.NoError(t, err)
			require.Greater(t, rowCount, 1)

			testutil_testdata.VerifySQLTableColumnValues(
				t,
				ctx,
				mysql.Source.DB,
				mysql.Target.DB,
				alltypesSchema,
				"json_data",
				"mysql",
				[]string{"id"},
			)
			testutil_testdata.VerifySQLTableColumnValues(
				t,
				ctx,
				mysql.Source.DB,
				mysql.Target.DB,
				alltypesSchema,
				"all_data_types",
				"mysql",
				[]string{"id"},
			)
			testutil_testdata.VerifySQLTableColumnValues(
				t,
				ctx,
				mysql.Source.DB,
				mysql.Target.DB,
				alltypesSchema,
				"generated_table",
				"mysql",
				[]string{"id"},
			)
		})

		t.Cleanup(func() {
			err := mysql.TearDown(ctx)
			if err != nil {
				t.Fatal(err)
			}
		})
	})

	t.Run("dynamodb", func(t *testing.T) {
		t.Parallel()
		dynamo, err := tcdynamodb.NewDynamoDBTestSyncContainer(
			ctx,
			t,
			[]tcdynamodb.Option{},
			[]tcdynamodb.Option{},
		)
		if err != nil {
			t.Fatal(err)
		}

		sourceConn := tchusonymapi.CreateDynamoDBConnection(
			ctx,
			t,
			husonymApi.OSSUnauthenticatedLicensedClients.Connections(),
			accountId,
			"dynamo-source",
			dynamo.Source.URL,
			dynamo.Source.Credentials,
		)

		t.Run("dynamodb_sync", func(t *testing.T) {
			t.Parallel()

			tableName := "test-sync-source"
			primaryKey := "id"

			err = dynamo.Source.SetupDynamoDbTable(ctx, tableName, primaryKey)
			if err != nil {
				t.Fatal(err)
			}

			err = dynamo.Target.SetupDynamoDbTable(ctx, tableName, primaryKey)
			if err != nil {
				t.Fatal(err)
			}

			testData := []map[string]dyntypes.AttributeValue{
				{
					"id": &dyntypes.AttributeValueMemberS{Value: "1"},
					"a":  &dyntypes.AttributeValueMemberBOOL{Value: true},
					"NestedMap": &dyntypes.AttributeValueMemberM{
						Value: map[string]dyntypes.AttributeValue{
							"Level1": &dyntypes.AttributeValueMemberM{
								Value: map[string]dyntypes.AttributeValue{
									"Level2": &dyntypes.AttributeValueMemberM{
										Value: map[string]dyntypes.AttributeValue{
											"Attribute1": &dyntypes.AttributeValueMemberS{
												Value: "Value1",
											},
											"NumberSet": &dyntypes.AttributeValueMemberNS{
												Value: []string{"1", "2", "3"},
											},
											"BinaryData": &dyntypes.AttributeValueMemberB{
												Value: []byte("U29tZUJpbmFyeURhdGE="),
											},
											"Level3": &dyntypes.AttributeValueMemberM{
												Value: map[string]dyntypes.AttributeValue{
													"Attribute2": &dyntypes.AttributeValueMemberS{
														Value: "Value2",
													},
													"StringSet": &dyntypes.AttributeValueMemberSS{
														Value: []string{"Item1", "Item2", "Item3"},
													},
													"BinarySet": &dyntypes.AttributeValueMemberBS{
														Value: [][]byte{
															[]byte("U29tZUJpbmFyeQ=="),
															[]byte("QW5vdGhlckJpbmFyeQ=="),
														},
													},
													"Level4": &dyntypes.AttributeValueMemberM{
														Value: map[string]dyntypes.AttributeValue{
															"Attribute3": &dyntypes.AttributeValueMemberS{
																Value: "Value3",
															},
															"Boolean": &dyntypes.AttributeValueMemberBOOL{
																Value: true,
															},
															"MoreBinaryData": &dyntypes.AttributeValueMemberB{
																Value: []byte(
																	"TW9yZUJpbmFyeURhdGE=",
																),
															},
															"MoreBinarySet": &dyntypes.AttributeValueMemberBS{
																Value: [][]byte{
																	[]byte("TW9yZUJpbmFyeQ=="),
																	[]byte(
																		"QW5vdGhlck1vcmVCaW5hcnk=",
																	),
																},
															},
														},
													},
												},
											},
										},
									},
								},
							},
						},
					},
				},
				{
					"id": &dyntypes.AttributeValueMemberS{Value: "2"},
					"a":  &dyntypes.AttributeValueMemberBOOL{Value: false},
				},
				{
					"id":   &dyntypes.AttributeValueMemberS{Value: "3"},
					"name": &dyntypes.AttributeValueMemberS{Value: "test3"},
				},
				{
					"id":   &dyntypes.AttributeValueMemberS{Value: "4"},
					"name": &dyntypes.AttributeValueMemberS{Value: "test4"},
				},
			}

			err = dynamo.Source.InsertDynamoDBRecords(ctx, tableName, testData)
			if err != nil {
				t.Fatal(err)
			}

			testlogger := testutil.GetTestLogger(t)
			cmdConfig := &cmdConfig{
				Source: &sourceConfig{
					ConnectionId: sourceConn.Id,
				},
				AwsDynamoDbDestination: &dynamoDbDestinationConfig{
					AwsCredConfig: &AwsCredConfig{
						Region:          "us-west-2",
						AccessKeyID:     dynamo.Target.Credentials.AccessKeyId,
						SecretAccessKey: dynamo.Target.Credentials.SecretAccessKey,
						SessionToken:    dynamo.Target.Credentials.SessionToken,
						Endpoint:        &dynamo.Target.URL,
					},
				},
				OutputType: &outputType,
				AccountId:  &accountId,
			}
			sync := &clisync{
				connectiondataclient: conndataclient,
				connectionclient:     connclient,
				sqlmanagerclient:     sqlmanagerclient,
				ctx:                  ctx,
				logger:               testlogger,
				cmd:                  cmdConfig,
				connmanager:          connmanager,
				session:              connectionmanager.NewUniqueSession(),
			}
			err := sync.configureAndRunSync()
			require.NoError(t, err)

			out, err := dynamo.Source.Client.Scan(ctx, &dynamodb.ScanInput{
				TableName: &tableName,
			})
			require.NoError(t, err)
			// Verify data was synced
			out, err = dynamo.Target.Client.Scan(ctx, &dynamodb.ScanInput{
				TableName: &tableName,
			})
			require.NoError(t, err)
			require.Equal(t, int32(4), out.Count)
		})

		t.Cleanup(func() {
			err := dynamo.TearDown(ctx)
			if err != nil {
				t.Fatal(err)
			}
		})
	})

	t.Cleanup(func() {
		err = husonymApi.TearDown(ctx)
		if err != nil {
			t.Fatal(err)
		}
	})
}
