package sync_activity

// athanor.go — aiguillage vers le moteur d'anonymisation Athanor, en alternative
// au stream Benthos, activé par le flag ENABLE_ATHANOR_ENGINE.
//
// Portée volontairement bornée pour ce premier câblage : table complète, une
// seule destination, source et destination de MÊME SGBD (PostgreSQL ou MySQL),
// sans subsetting (WHERE) ni upsert/onConflict. Ces éléments — déjà gérés par le
// chemin Benthos — seront ajoutés lors de la migration par route.

import (
	"context"
	"fmt"
	"log/slog"

	"connectrpc.com/connect"
	mgmtv1alpha1 "github.com/Groupe-Hevea/neosync/backend/gen/go/protos/mgmt/v1alpha1"
	connectionmanager "github.com/Groupe-Hevea/neosync/internal/connection-manager"
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

	logger.Info("moteur=athanor : anonymisation de table",
		"schema", metadata.Schema,
		"table", metadata.Table,
		"colonnes", len(mappings),
		"srcConn", srcConnID,
		"dstConn", dstConnID,
	)

	return runner.RunTable(ctx, srcDB, dstDB, dialect, mappings, metadata.Schema, metadata.Table, athanorBatchSize)
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
	default:
		return nil, fmt.Errorf("athanor: dialecte non supporté (seuls PostgreSQL et MySQL le sont)")
	}
}
