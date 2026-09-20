package sync_activity

// athanor.go — aiguillage vers le moteur d'anonymisation Athanor, en alternative
// au stream Benthos, choisi par job.
//
// Athanor exécute le plan neutre calculé par GenerateBenthosConfigs (subset,
// pagination, passes, clés transformées suivies par les clés étrangères). Restent à
// porter : destinations multiples, SGBD différents entre source et destination, passes
// de mise à jour hors MySQL.

import (
	"context"
	"fmt"
	"log/slog"

	"connectrpc.com/connect"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	connectionmanager "github.com/fishtre-compagnie/husonym/internal/connection-manager"
	continuation_token "github.com/fishtre-compagnie/husonym/internal/continuation-token"
	"github.com/fishtre-compagnie/husonym/internal/runconfigs"
	"github.com/fishtre-compagnie/husonym/internal/tableplan"
	"github.com/fishtre-compagnie/husonym/worker/pkg/athanor/consistency"
	"github.com/fishtre-compagnie/husonym/worker/pkg/athanor/runner"
	"github.com/fishtre-compagnie/husonym/worker/pkg/athanor/sqlio"
	te "github.com/fishtre-compagnie/husonym/worker/pkg/benthos/transformer_executor"
	"github.com/fishtre-compagnie/husonym/worker/pkg/benthos/transformers"
	"github.com/fishtre-compagnie/husonym/worker/pkg/workflows/datasync/activities/shared"
	"github.com/google/uuid"
)

const athanorBatchSize = 1000

// jobIDFromRunID extrait le jobId d'un JobRunId de forme "<jobId>-<timestamp>",
// où jobId est un UUID (36 caractères).
func jobIDFromRunID(runID string) (string, error) {
	const uuidLen = 36
	if len(runID) < uuidLen {
		return "", fmt.Errorf("athanor: JobRunId inattendu %q", runID)
	}
	id := runID[:uuidLen]
	if _, err := uuid.Parse(id); err != nil {
		return "", fmt.Errorf("athanor: impossible d'extraire le jobId de %q: %w", runID, err)
	}
	return id, nil
}

// useAthanorForJob décide, PAR JOB, si Athanor doit traiter ce run. Priorité au
// champ `engine` du job (réglé dans l'UI, WorkflowOptions) ; à défaut
// (UNSPECIFIED) on retombe sur la policy de déploiement (AthanorConfig.Policy,
// variables d'env).
//
// Une lecture du job qui échoue est une erreur, jamais un repli : le moteur doit
// être le même pour toutes les tables d'un run. Les deux moteurs dérivent des
// valeurs anonymisées différentes, donc une seule table passée sur l'autre moteur
// — le temps d'un timeout, ou entre deux tentatives d'une même activité — casse
// les clés étrangères entre les tables du run. Mieux vaut refaire la tentative.
func (a *Activity) useAthanorForJob(ctx context.Context, jobRunID string) (bool, error) {
	jobID, _ := jobIDFromRunID(jobRunID)
	if jobID == "" {
		return a.athanor.Policy.EnabledFor(jobID), nil
	}
	resp, err := a.jobclient.GetJob(ctx, connect.NewRequest(&mgmtv1alpha1.GetJobRequest{Id: jobID}))
	if err != nil {
		return false, fmt.Errorf("athanor: lecture du moteur du job %s: %w", jobID, err)
	}
	return a.athanor.Policy.UsesAthanor(resp.Msg.GetJob()), nil
}

// getTablePlan loads the engine-neutral plan of this table sync. It returns nil, and no
// error, when the run has none: only SQL sources get a plan.
func (a *Activity) getTablePlan(ctx context.Context, req *SyncTableRequest) (*tableplan.TablePlan, error) {
	resp, err := a.jobclient.GetRunContext(ctx, connect.NewRequest(&mgmtv1alpha1.GetRunContextRequest{
		Id: &mgmtv1alpha1.RunContextKey{
			JobRunId:   req.JobRunId,
			ExternalId: shared.GetTablePlanExternalId(req.Id),
			AccountId:  req.AccountId,
		},
	}))
	if connect.CodeOf(err) == connect.CodeNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("athanor: lecture du plan de %s: %w", req.Id, err)
	}
	return tableplan.Unmarshal(resp.Msg.GetValue())
}

func (a *Activity) runAthanor(
	ctx context.Context,
	req *SyncTableRequest,
	plan *tableplan.TablePlan,
	attempt int32,
	session connectionmanager.SessionInterface,
	getConnectionById func(connectionId string) (connectionmanager.ConnectionInput, error),
	logger *slog.Logger,
) (*SyncTableResponse, error) {
	// 1) Job + mappings. Le JobRunId a la forme "<jobId>-<timestamp>" ; on en
	// extrait le jobId et on lit le job directement (GetJob = lecture en base),
	// sans passer par GetJobRun (qui interroge Temporal et échoue en cours de run).
	jobID, err := jobIDFromRunID(req.JobRunId)
	if err != nil {
		return nil, err
	}
	jobResp, err := a.jobclient.GetJob(ctx, connect.NewRequest(&mgmtv1alpha1.GetJobRequest{Id: jobID}))
	if err != nil {
		return nil, fmt.Errorf("athanor: récupération du job %q: %w", jobID, err)
	}
	job := jobResp.Msg.GetJob()

	// 2) Source/destination connections, derived from the job (not the RunContext).
	srcConnID, err := sourceConnectionID(job.GetSource())
	if err != nil {
		return nil, err
	}
	dests := job.GetDestinations()
	if len(dests) != 1 {
		return nil, fmt.Errorf("athanor: %d destination(s) ; le câblage initial en gère une seule", len(dests))
	}
	dstConnID := dests[0].GetConnectionId()

	srcInput, err := getConnectionById(srcConnID)
	if err != nil {
		//nolint:misspell // message produit, rédigé en français
		return nil, fmt.Errorf("athanor: connexion source %q: %w", srcConnID, err)
	}
	dstInput, err := getConnectionById(dstConnID)
	if err != nil {
		//nolint:misspell // message produit, rédigé en français
		return nil, fmt.Errorf("athanor: connexion destination %q: %w", dstConnID, err)
	}

	// 3) Dialecte : source et destination doivent être du même SGBD supporté.
	dialect, err := homogeneousDialect(srcInput, dstInput)
	if err != nil {
		return nil, err
	}

	// 4) Passes de mise à jour. Quand la destination permet de couper la
	// vérification des clés étrangères, la passe d'insertion écrit déjà toutes les
	// colonnes, sauf les clés étrangères qui suivent une clé transformée écrite plus
	// tard (cycle, auto-référence) : seule une passe qui en porte est exécutée.
	_, _, fkChecksOff := dialect.ForeignKeyChecksStatements()
	runPage := runner.RunTablePage
	if plan.RunType == runconfigs.RunTypeUpdate {
		if !fkChecksOff {
			return nil, fmt.Errorf("athanor: passe de mise à jour de %s.%s non supportée sur %s",
				plan.Schema, plan.Table, dialect.Driver())
		}
		if !runner.UpdateFollowsTransformedKey(plan) {
			logger.Info("moteur=athanor : passe de mise à jour déjà couverte par la passe d'insertion",
				"schema", plan.Schema, "table", plan.Table, "colonnes", plan.Columns)
			return &SyncTableResponse{}, nil
		}
		runPage = runner.RunUpdatePage
	}

	// 5) Handles SQL (le SqlDbtx satisfait Querier, Execer et BeginTx).
	srcDB, err := a.sqlconnmanager.GetConnection(session, srcInput, logger)
	if err != nil {
		return nil, fmt.Errorf("athanor: ouverture de la source: %w", err)
	}
	dstDB, err := a.sqlconnmanager.GetConnection(session, dstInput, logger)
	if err != nil {
		return nil, fmt.Errorf("athanor: ouverture de la destination: %w", err)
	}

	// Gestion des conflits de clé (onConflict) dérivée des options de destination.
	// Une nouvelle tentative réécrit une page déjà partiellement écrite : comme
	// Benthos, on ignore alors les lignes déjà présentes.
	wc := writeConfigForDest(dests[0])
	if attempt > 1 && wc.OnConflict == sqlio.ConflictNone {
		wc.OnConflict = sqlio.ConflictDoNothing
	}
	wc.DisableForeignKeyChecks = fkChecksOff

	var after []any
	if req.ContinuationToken != nil {
		token, terr := continuation_token.FromTokenString(*req.ContinuationToken)
		if terr != nil {
			return nil, fmt.Errorf("athanor: jeton de continuation illisible: %w", terr)
		}
		after = token.Contents.LastReadOrderValues
	}

	// Cohérence déterministe (RFC §8) : la même valeur d'entrée produit la même
	// sortie partout dans la portée choisie pour le job (run par défaut).
	deriver, err := consistencyDeriver(a.athanor.ConsistencyKey, job, req.JobRunId)
	if err != nil {
		return nil, err
	}

	logger.Info("moteur=athanor : anonymisation de table",
		"schema", plan.Schema,
		"table", plan.Table,
		"srcConn", srcConnID,
		"dstConn", dstConnID,
		"reprise", after != nil,
		"onConflict", wc.OnConflict,
		"clésÉtrangèresCoupées", wc.DisableForeignKeyChecks,
		"cohérence", job.GetWorkflowOptions().GetConsistencyScope().String(),
	)

	// Mêmes capacités que le chemin Benthos : transformers définis par l'utilisateur
	// et TransformPiiText (y compris depuis le JavaScript), via l'API liée au compte.
	resolver := te.NewUserDefinedTransformerResolver(a.transformerclient)
	piiTextApi := transformers.NewAccountAwareAnonymizationPiiTextApi(a.anonymizationClient, req.AccountId)
	env := &runner.TransformEnv{
		Resolver:   resolver,
		PiiTextApi: piiTextApi,
		Logger:     logger,
		ExecOptions: []te.TransformerExecutorOption{
			te.WithLogger(logger),
			te.WithUserDefinedTransformerResolver(resolver),
			te.WithTransformPiiTextApi(piiTextApi),
		},
	}

	res, err := runPage(ctx, srcDB, dstDB, dialect, &runner.TablePage{
		Plan:             plan,
		Mappings:         job.GetMappings(),
		BatchSize:        athanorBatchSize,
		Write:            wc,
		Deriver:          deriver,
		AfterOrderValues: after,
		Env:              env,
		Keys:             newRedisKeyStore(a.redisclient),
	})
	if err != nil {
		return nil, err
	}
	logger.Info("moteur=athanor : page écrite", "lignes", res.RowsRead, "pageSuivante", res.HasMore)
	if res.RowsDiscarded > 0 {
		logger.Warn("moteur=athanor : lignes écartées, parent obligatoire absent de la destination",
			"schema", plan.Schema, "table", plan.Table, "lignes", res.RowsDiscarded)
	}

	resp := &SyncTableResponse{}
	if res.HasMore {
		token, terr := continuation_token.NewFromContents(continuation_token.NewContents(res.LastOrderValues)).Encode()
		if terr != nil {
			return nil, fmt.Errorf("athanor: %s.%s: %w", plan.Schema, plan.Table, terr)
		}
		resp.ContinuationToken = &token
	}
	return resp, nil
}

// sourceConnectionID extracts the source connection id for the job's dialect.
func sourceConnectionID(src *mgmtv1alpha1.JobSource) (string, error) {
	opts := src.GetOptions()
	switch {
	case opts.GetPostgres() != nil:
		return opts.GetPostgres().GetConnectionId(), nil
	case opts.GetMysql() != nil:
		return opts.GetMysql().GetConnectionId(), nil
	case opts.GetMssql() != nil:
		return opts.GetMssql().GetConnectionId(), nil
	default:
		return "", fmt.Errorf("athanor: type de source non supporté (attendu postgres, mysql ou mssql)")
	}
}

// homogeneousDialect renvoie le dialecte commun si source et destination sont du
// même SGBD supporté (PostgreSQL ou MySQL), sinon une erreur explicite.
func homogeneousDialect(src, dst connectionmanager.ConnectionInput) (sqlio.Dialect, error) {
	sd, err := dialectFor(src)
	if err != nil {
		return nil, fmt.Errorf("source: %w", err)
	}
	dd, err := dialectFor(dst)
	if err != nil {
		return nil, fmt.Errorf("destination: %w", err)
	}
	if fmt.Sprintf("%T", sd) != fmt.Sprintf("%T", dd) {
		return nil, fmt.Errorf("athanor: source et destination de SGBD différents non supportés pour l'instant")
	}
	return sd, nil
}

func dialectFor(conn connectionmanager.ConnectionInput) (sqlio.Dialect, error) {
	switch conn.GetConnectionConfig().GetConfig().(type) {
	case *mgmtv1alpha1.ConnectionConfig_PgConfig:
		return sqlio.PostgresDialect{}, nil
	case *mgmtv1alpha1.ConnectionConfig_MysqlConfig:
		return sqlio.MySQLDialect{}, nil
	case *mgmtv1alpha1.ConnectionConfig_MssqlConfig:
		return sqlio.MSSQLDialect{}, nil
	default:
		return nil, fmt.Errorf("athanor: dialecte non supporté (seuls PostgreSQL, MySQL et SQL Server le sont)")
	}
}

// consistencyDeriver builds the deterministic consistency deriver (RFC §8) for the
// job's scope. The key is mandatory: anyone who knows it can recover low-entropy
// values (phone numbers, first names) by brute force, so a built-in default would
// make every output reversible.
//
// The scope always embeds the run, job or account id: outputs never match across
// two runs (run), two jobs (job) or two accounts (account).
func consistencyDeriver(key string, job *mgmtv1alpha1.Job, jobRunID string) (*consistency.Deriver, error) {
	if key == "" {
		return nil, fmt.Errorf("athanor: ATHANOR_CONSISTENCY_KEY n'est pas défini ; la clé de dérivation est obligatoire")
	}
	var scope string
	switch s := job.GetWorkflowOptions().GetConsistencyScope(); s {
	case mgmtv1alpha1.ConsistencyScope_CONSISTENCY_SCOPE_UNSPECIFIED,
		mgmtv1alpha1.ConsistencyScope_CONSISTENCY_SCOPE_RUN:
		scope = "run:" + jobRunID
	case mgmtv1alpha1.ConsistencyScope_CONSISTENCY_SCOPE_JOB:
		scope = "job:" + job.GetId()
	case mgmtv1alpha1.ConsistencyScope_CONSISTENCY_SCOPE_ACCOUNT:
		scope = "account:" + job.GetAccountId()
	default:
		return nil, fmt.Errorf("athanor: portée de cohérence inconnue %v", s)
	}
	return consistency.New([]byte(key), scope), nil
}

// writeConfigForDest dérive la politique d'écriture (gestion des conflits de clé)
// des options de la destination du job. Le truncate éventuel est géré en amont
// par l'activité d'init de schéma, indépendamment du moteur.
func writeConfigForDest(dst *mgmtv1alpha1.JobDestination) runner.WriteConfig {
	opts := dst.GetOptions()
	switch {
	case opts.GetMysqlOptions() != nil:
		return runner.WriteConfig{
			OnConflict:               conflictFromMysql(opts.GetMysqlOptions().GetOnConflict()),
			SkipForeignKeyViolations: opts.GetMysqlOptions().GetSkipForeignKeyViolations(),
		}
	case opts.GetPostgresOptions() != nil:
		return runner.WriteConfig{
			OnConflict:               conflictFromPostgres(opts.GetPostgresOptions().GetOnConflict()),
			SkipForeignKeyViolations: opts.GetPostgresOptions().GetSkipForeignKeyViolations(),
		}
	case opts.GetMssqlOptions() != nil:
		return runner.WriteConfig{
			OnConflict:               conflictFromMssql(opts.GetMssqlOptions().GetOnConflict()),
			SkipForeignKeyViolations: opts.GetMssqlOptions().GetSkipForeignKeyViolations(),
		}
	default:
		return runner.WriteConfig{}
	}
}

func conflictFromMysql(oc *mgmtv1alpha1.MysqlOnConflictConfig) sqlio.ConflictAction {
	if oc == nil {
		return sqlio.ConflictNone
	}
	if oc.GetUpdate() != nil {
		return sqlio.ConflictDoUpdate
	}
	if oc.GetNothing() != nil {
		return sqlio.ConflictDoNothing
	}
	return sqlio.ConflictNone
}

func conflictFromPostgres(oc *mgmtv1alpha1.PostgresOnConflictConfig) sqlio.ConflictAction {
	if oc == nil {
		return sqlio.ConflictNone
	}
	if oc.GetUpdate() != nil {
		return sqlio.ConflictDoUpdate
	}
	if oc.GetNothing() != nil {
		return sqlio.ConflictDoNothing
	}
	return sqlio.ConflictNone
}

// conflictFromMssql : SQL Server ne propose que « do nothing » (pas de MERGE/upsert
// dans le query-builder partagé, comme le chemin Benthos).
func conflictFromMssql(oc *mgmtv1alpha1.MssqlOnConflictConfig) sqlio.ConflictAction {
	if oc == nil {
		return sqlio.ConflictNone
	}
	if oc.GetDoNothing() {
		return sqlio.ConflictDoNothing
	}
	return sqlio.ConflictNone
}
