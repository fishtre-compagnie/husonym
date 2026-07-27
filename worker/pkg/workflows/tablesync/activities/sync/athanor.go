package sync_activity

// athanor.go — aiguillage vers le moteur d'anonymisation Athanor, en alternative
// au stream Benthos, activé par le flag ENABLE_ATHANOR_ENGINE.
//
// Portée volontairement bornée pour ce premier câblage : une seule destination,
// source et destination de MÊME SGBD (PostgreSQL ou MySQL). Le subsetting (WHERE)
// est géré ; restent à porter l'upsert/onConflict, les destinations multiples et
// les SGBD hétérogènes.

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"connectrpc.com/connect"
	mgmtv1alpha1 "github.com/Groupe-Hevea/neosync/backend/gen/go/protos/mgmt/v1alpha1"
	connectionmanager "github.com/Groupe-Hevea/neosync/internal/connection-manager"
	"github.com/Groupe-Hevea/neosync/worker/pkg/athanor/consistency"
	"github.com/Groupe-Hevea/neosync/worker/pkg/athanor/runner"
	"github.com/Groupe-Hevea/neosync/worker/pkg/athanor/sqlio"
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

func (a *Activity) runAthanor(
	ctx context.Context,
	req *SyncTableRequest,
	metadata *SyncMetadata,
	session connectionmanager.SessionInterface,
	getConnectionById func(connectionId string) (connectionmanager.ConnectionInput, error),
	logger *slog.Logger,
) error {
	// 1) Job + mappings. Le JobRunId a la forme "<jobId>-<timestamp>" ; on en
	// extrait le jobId et on lit le job directement (GetJob = lecture en base),
	// sans passer par GetJobRun (qui interroge Temporal et échoue en cours de run).
	jobID, err := jobIDFromRunID(req.JobRunId)
	if err != nil {
		return err
	}
	jobResp, err := a.jobclient.GetJob(ctx, connect.NewRequest(&mgmtv1alpha1.GetJobRequest{Id: jobID}))
	if err != nil {
		return fmt.Errorf("athanor: récupération du job %q: %w", jobID, err)
	}
	job := jobResp.Msg.GetJob()
	mappings := job.GetMappings()

	// 2) Connexions source/destination, dérivées du job (pas du RunContext).
	srcConnID, err := sourceConnectionID(job.GetSource())
	if err != nil {
		return err
	}
	dests := job.GetDestinations()
	if len(dests) != 1 {
		return fmt.Errorf("athanor: %d destination(s) ; le câblage initial en gère une seule", len(dests))
	}
	dstConnID := dests[0].GetConnectionId()

	srcInput, err := getConnectionById(srcConnID)
	if err != nil {
		return fmt.Errorf("athanor: connexion source %q: %w", srcConnID, err)
	}
	dstInput, err := getConnectionById(dstConnID)
	if err != nil {
		return fmt.Errorf("athanor: connexion destination %q: %w", dstConnID, err)
	}

	// 3) Dialecte : source et destination doivent être du même SGBD supporté.
	dialect, err := homogeneousDialect(srcInput, dstInput)
	if err != nil {
		return err
	}

	// 4) Handles SQL (le SqlDbtx satisfait à la fois Querier et Execer).
	srcDB, err := a.sqlconnmanager.GetConnection(session, srcInput, logger)
	if err != nil {
		return fmt.Errorf("athanor: ouverture de la source: %w", err)
	}
	dstDB, err := a.sqlconnmanager.GetConnection(session, dstInput, logger)
	if err != nil {
		return fmt.Errorf("athanor: ouverture de la destination: %w", err)
	}

	// Subsetting : clause WHERE éventuelle configurée pour cette table.
	where := runner.WhereForTable(job.GetSource(), metadata.Schema, metadata.Table)

	// Gestion des conflits de clé (onConflict) dérivée des options de destination.
	wc := writeConfigForDest(dests[0])

	// Cohérence déterministe (RFC §8) : dériveur construit pour ce run. La même
	// valeur d'entrée produira la même sortie, sur toutes les lignes/tables/runs.
	deriver := consistencyDeriver()

	logger.Info("moteur=athanor : anonymisation de table",
		"schema", metadata.Schema,
		"table", metadata.Table,
		"colonnes", len(mappings),
		"srcConn", srcConnID,
		"dstConn", dstConnID,
		"where", where,
		"onConflict", wc.OnConflict,
		"cohérence", "déterministe",
	)

	return runner.RunTable(ctx, srcDB, dstDB, dialect, mappings, metadata.Schema, metadata.Table, where, athanorBatchSize, wc, deriver)
}

// sourceConnectionID extrait l'id de connexion source selon le dialecte du job.
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

// consistencyDeriver construit le dériveur de cohérence déterministe (RFC §8).
// La clé de projet vient de ATHANOR_CONSISTENCY_KEY ; à défaut, une clé de démo
// (bouchon en attendant le Key Service, RFC §8.4). Scope "org" = cohérence
// maximale (même valeur → même sortie dans toute l'organisation, inter-runs).
func consistencyDeriver() *consistency.Deriver {
	key := os.Getenv("ATHANOR_CONSISTENCY_KEY")
	if key == "" {
		key = "husonym-athanor-demo-key"
	}
	return consistency.New([]byte(key), "org")
}

// writeConfigForDest dérive la politique d'écriture (gestion des conflits de clé)
// des options de la destination du job. Le truncate éventuel est géré en amont
// par l'activité d'init de schéma, indépendamment du moteur.
func writeConfigForDest(dst *mgmtv1alpha1.JobDestination) runner.WriteConfig {
	opts := dst.GetOptions()
	switch {
	case opts.GetMysqlOptions() != nil:
		return runner.WriteConfig{OnConflict: conflictFromMysql(opts.GetMysqlOptions().GetOnConflict())}
	case opts.GetPostgresOptions() != nil:
		return runner.WriteConfig{OnConflict: conflictFromPostgres(opts.GetPostgresOptions().GetOnConflict())}
	case opts.GetMssqlOptions() != nil:
		return runner.WriteConfig{OnConflict: conflictFromMssql(opts.GetMssqlOptions().GetOnConflict())}
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
	if oc.GetNothing() != nil || oc.GetDoNothing() {
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
	if oc.GetNothing() != nil || oc.GetDoNothing() {
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
