'use client';
import ConnectionIcon from '@/components/connections/ConnectionIcon';
import DynamoDbConnectionForm from '@/components/connections/forms/dynamodb/DynamoDbConnectionForm';
import GcpCloudStorageConnectionForm from '@/components/connections/forms/gcp-cloud-storage/GcpCloudStorageConnectionForm';
import MongoDbConnectionForm from '@/components/connections/forms/mongodb/MongoDbConnectionForm';
import MysqlConnectionForm from '@/components/connections/forms/mysql/MysqlConnectionForm';
import OpenAiConnectionForm from '@/components/connections/forms/openai/OpenAiConnectionForm';
import PostgresConnectionForm from '@/components/connections/forms/postgres/PostgresConnectionForm';
import AwsS3ConnectionForm from '@/components/connections/forms/s3/AwsS3ConnectionForm';
import SqlServerConnectionForm from '@/components/connections/forms/sql-server/SqlServerConnectionForm';
import { PageHeader as DsPageHeader, Pill } from '@/components/ds';
import { Connection, PostgresConnectionConfig } from '@husonym/sdk';
import { ReactElement } from 'react';
import ModeView from './ModeView';

/** The head of a connection's page: its name, its engine and its id, and its actions. */
function PageHeader(props: {
  header: string;
  name?: string;
  leftIcon?: ReactElement;
  extraHeading?: ReactElement;
  subHeadings?: ReactElement;
}): ReactElement {
  const { header, name, leftIcon, extraHeading, subHeadings } = props;
  return (
    <div className="flex flex-col gap-1.5">
      <DsPageHeader
        title={
          <span className="flex min-w-0 items-center gap-3">
            {leftIcon && (
              <span className="grid size-10 shrink-0 place-items-center rounded-[10px] bg-surface-2 [&_svg]:size-6!">
                {leftIcon}
              </span>
            )}
            <span className="truncate">{name || header}</span>
          </span>
        }
        description={name ? <Pill tone="brand">{header}</Pill> : undefined}
        actions={extraHeading}
      />
      {/* The id holds a copy button, which a paragraph cannot hold. */}
      {subHeadings && <div className="font-mono text-xs">{subHeadings}</div>}
    </div>
  );
}

interface ConnectionComponent {
  body: ReactElement;
  header: ReactElement;
}

type BaseConnectionComponentDetailsProps = {
  connection?: Connection;
  extraPageHeading?: ReactElement;
  subHeading?: ReactElement;
};

type ViewModeProps = BaseConnectionComponentDetailsProps & {
  mode?: 'view';
  onSaved?: never;
};

type EditModeProps = BaseConnectionComponentDetailsProps & {
  mode: 'edit';
  onSaved(connection: Connection): Promise<void> | void;
};

type CloneModeProps = BaseConnectionComponentDetailsProps & {
  mode: 'clone';
  connection?: Connection;
  onSaved(connection: Connection): Promise<void> | void;
};

type GetConnectionComponentDetailsProps =
  ViewModeProps | EditModeProps | CloneModeProps;

function getPgHeaderType(
  connection: PostgresConnectionConfig
): 'generic' | 'neon' {
  switch (connection.connectionConfig.case) {
    case 'connection':
      if (connection.connectionConfig.value.host.includes('neon')) {
        return 'neon';
      }
      return 'generic';
    case 'url':
      if (connection.connectionConfig.value.includes('neon')) {
        return 'neon';
      }
      return 'generic';
    case 'urlFromEnv':
      return 'generic';
    default:
      return 'generic';
  }
}

export function useGetConnectionComponentDetails(
  props: GetConnectionComponentDetailsProps
): ConnectionComponent {
  const {
    connection,

    onSaved,
    extraPageHeading,
    subHeading,
    mode = 'view',
  } = props;

  async function onSuccess(connection: Connection): Promise<void> {
    if (onSaved) {
      onSaved(connection);
    }
  }

  switch (connection?.connectionConfig?.config?.case) {
    case 'pgConfig': {
      const value = connection.connectionConfig.config.value;
      const headerType = getPgHeaderType(value);

      return {
        header: (
          <PageHeader
            name={connection.name}
            header={headerType == 'neon' ? 'Neon' : 'PostgreSQL'}
            leftIcon={
              headerType == 'neon' ? (
                <ConnectionIcon
                  connectionType="pgConfig"
                  connectionTypeVariant="neon"
                />
              ) : (
                <ConnectionIcon connectionType="pgConfig" />
              )
            }
            extraHeading={extraPageHeading}
            subHeadings={subHeading}
          />
        ),
        body: (
          <ModeView
            mode={mode}
            view={() => (
              <PostgresConnectionForm mode="view" connection={connection} />
            )}
            edit={() => (
              <PostgresConnectionForm
                mode="edit"
                connection={connection}
                onSuccess={onSuccess}
              />
            )}
            clone={() => (
              <PostgresConnectionForm
                mode="clone"
                connection={connection}
                onSuccess={onSuccess}
              />
            )}
          />
        ),
      };
    }

    case 'mysqlConfig': {
      return {
        header: (
          <PageHeader
            name={connection.name}
            header="Mysql"
            leftIcon={<ConnectionIcon connectionType="mysqlConfig" />}
            extraHeading={extraPageHeading}
            subHeadings={subHeading}
          />
        ),
        body: (
          <ModeView
            mode={mode}
            view={() => (
              <MysqlConnectionForm mode="view" connection={connection} />
            )}
            edit={() => (
              <MysqlConnectionForm
                mode="edit"
                connection={connection}
                onSuccess={onSuccess}
              />
            )}
            clone={() => (
              <MysqlConnectionForm
                mode="clone"
                connection={connection}
                onSuccess={onSuccess}
              />
            )}
          />
        ),
      };
    }

    case 'awsS3Config':
      return {
        header: (
          <PageHeader
            name={connection.name}
            header="AWS S3"
            leftIcon={<ConnectionIcon connectionType="awsS3Config" />}
            extraHeading={extraPageHeading}
            subHeadings={subHeading}
          />
        ),
        body: (
          <ModeView
            mode={mode}
            view={() => (
              <AwsS3ConnectionForm mode="view" connection={connection} />
            )}
            edit={() => (
              <AwsS3ConnectionForm
                mode="edit"
                connection={connection}
                onSuccess={onSuccess}
              />
            )}
            clone={() => (
              <AwsS3ConnectionForm
                mode="clone"
                connection={connection}
                onSuccess={onSuccess}
              />
            )}
          />
        ),
      };
    case 'openaiConfig':
      return {
        header: (
          <PageHeader
            name={connection.name}
            header="OpenAI"
            leftIcon={<ConnectionIcon connectionType="openaiConfig" />}
            extraHeading={extraPageHeading}
            subHeadings={subHeading}
          />
        ),
        body: (
          <ModeView
            mode={mode}
            view={() => (
              <OpenAiConnectionForm mode="view" connection={connection} />
            )}
            edit={() => (
              <OpenAiConnectionForm
                mode="edit"
                connection={connection}
                onSuccess={onSuccess}
              />
            )}
            clone={() => (
              <OpenAiConnectionForm
                mode="clone"
                connection={connection}
                onSuccess={onSuccess}
              />
            )}
          />
        ),
      };
    case 'mongoConfig':
      if (
        connection.connectionConfig.config.value.connectionConfig.case !== 'url'
      ) {
        return {
          header: <PageHeader header="Invalid MongoDB Connection" />,
          body: (
            <div>
              No connection component found for: (
              {connection?.name ?? 'unknown name'})
            </div>
          ),
        };
      }
      return {
        header: (
          <PageHeader
            name={connection.name}
            header="MongoDB"
            leftIcon={<ConnectionIcon connectionType="mongoConfig" />}
            extraHeading={extraPageHeading}
            subHeadings={subHeading}
          />
        ),
        body: (
          <ModeView
            mode={mode}
            view={() => (
              <MongoDbConnectionForm mode="view" connection={connection} />
            )}
            edit={() => (
              <MongoDbConnectionForm
                mode="edit"
                connection={connection}
                onSuccess={onSuccess}
              />
            )}
            clone={() => (
              <MongoDbConnectionForm
                mode="clone"
                connection={connection}
                onSuccess={onSuccess}
              />
            )}
          />
        ),
      };
    case 'gcpCloudstorageConfig': {
      return {
        header: (
          <PageHeader
            name={connection.name}
            header="GCP Cloud Storage"
            leftIcon={<ConnectionIcon connectionType="gcpCloudstorageConfig" />}
            extraHeading={extraPageHeading}
            subHeadings={subHeading}
          />
        ),
        body: (
          <ModeView
            mode={mode}
            view={() => (
              <GcpCloudStorageConnectionForm
                mode="view"
                connection={connection}
              />
            )}
            edit={() => (
              <GcpCloudStorageConnectionForm
                mode="edit"
                connection={connection}
                onSuccess={onSuccess}
              />
            )}
            clone={() => (
              <GcpCloudStorageConnectionForm
                mode="clone"
                connection={connection}
                onSuccess={onSuccess}
              />
            )}
          />
        ),
      };
    }
    case 'dynamodbConfig': {
      return {
        header: (
          <PageHeader
            name={connection.name}
            header="DynamoDB"
            leftIcon={<ConnectionIcon connectionType="dynamodbConfig" />}
            extraHeading={extraPageHeading}
            subHeadings={subHeading}
          />
        ),
        body: (
          <ModeView
            mode={mode}
            view={() => (
              <DynamoDbConnectionForm mode="view" connection={connection} />
            )}
            edit={() => (
              <DynamoDbConnectionForm
                mode="edit"
                connection={connection}
                onSuccess={onSuccess}
              />
            )}
            clone={() => (
              <DynamoDbConnectionForm
                mode="clone"
                connection={connection}
                onSuccess={onSuccess}
              />
            )}
          />
        ),
      };
    }
    case 'mssqlConfig': {
      return {
        header: (
          <PageHeader
            name={connection.name}
            header="Microsoft SQL Server"
            leftIcon={<ConnectionIcon connectionType="mssqlConfig" />}
            extraHeading={extraPageHeading}
            subHeadings={subHeading}
          />
        ),
        body: (
          <ModeView
            mode={mode}
            view={() => (
              <SqlServerConnectionForm mode="view" connection={connection} />
            )}
            edit={() => (
              <SqlServerConnectionForm
                mode="edit"
                connection={connection}
                onSuccess={onSuccess}
              />
            )}
            clone={() => (
              <SqlServerConnectionForm
                mode="clone"
                connection={connection}
                onSuccess={onSuccess}
              />
            )}
          />
        ),
      };
    }
    default:
      return {
        header: <PageHeader header="Unknown Connection" />,
        body: (
          <div>
            No connection component found for: (
            {connection?.name ?? 'unknown name'})
          </div>
        ),
      };
  }
}
