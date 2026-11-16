import { useAccount } from '@/components/providers/account-provider';
import { getErrorMessage } from '@/util/util';
import { create as createMessage } from '@bufbuild/protobuf';
import { useMutation } from '@connectrpc/connect-query';
import {
  Connection,
  ConnectionConfig,
  ConnectionService,
  CreateConnectionRequestSchema,
  UpdateConnectionRequestSchema,
} from '@husonym/sdk';
import { useEffect, useMemo, useState } from 'react';
import { useTranslations } from 'next-intl';
import { toast } from 'sonner';

interface CreateProps<T> {
  mode: 'create';
  buildConnectionConfig(formValues: T): ConnectionConfig;
  onSuccess(conn: Connection): Promise<void> | void;
}

interface EditProps<T> {
  mode: 'edit';
  connection: Connection;
  buildConnectionConfig(formValues: T): ConnectionConfig;
  toFormValues(connection: Connection): T | undefined;
  onSuccess(conn: Connection): Promise<void> | void;
}

interface ViewProps<T> {
  mode: 'view';
  connection: Connection;

  canViewSecrets?: boolean;
  toFormValues(connection: Connection): T | undefined;
}

interface CloneProps<T> {
  mode: 'clone';
  connection: Connection;
  toFormValues(connection: Connection): T | undefined;
  buildConnectionConfig(formValues: T): ConnectionConfig;
  onSuccess(conn: Connection): Promise<void> | void;
}

interface UseConnectionResult<T> {
  isLoading: boolean;
  initialValues: T | undefined;
  handleSubmit(values: T): Promise<void>;
  getValueWithSecrets(): Promise<T | undefined>;
  // only populated for view mode
  connectionId: string | undefined;
}

type Props<T> = CreateProps<T> | EditProps<T> | ViewProps<T> | CloneProps<T>;
export function useConnection<T extends { connectionName: string }>(
  props: Props<T>
): UseConnectionResult<T> {
  const { mode } = props;

  const t = useTranslations('connections.forms.common');
  const { account } = useAccount();

  const { mutateAsync: getConnection } = useMutation(
    ConnectionService.method.getConnection
  );
  const { mutateAsync: createConnection } = useMutation(
    ConnectionService.method.createConnection
  );
  const { mutateAsync: updateConnection } = useMutation(
    ConnectionService.method.updateConnection
  );
  const [isLoading, setIsLoading] = useState(true);
  const [initialValues, setInitialValues] = useState<T | undefined>(undefined);

  const connectionId = useMemo(() => {
    if (mode === 'view') {
      return props.connection.id;
    }
    return undefined;
  }, [mode]);

  useEffect(() => {
    async function loadValues(): Promise<void> {
      setIsLoading(true);
      try {
        if (mode === 'create') {
          setInitialValues(undefined);
          return;
        }

        // For view/edit/clone modes
        const values = props.toFormValues(props.connection);
        if (mode === 'view' || initialValues === undefined) {
          setInitialValues(values);
        }
      } finally {
        setIsLoading(false);
      }
    }
    loadValues();
  }, [
    mode,
    mode === 'view' ? props.connection : undefined,
    mode === 'clone' ? props.connection : undefined,
  ]);

  function handleOnSuccess(
    conn: Connection,
    mode: 'create' | 'clone' | 'edit',
    onSuccess: (conn: Connection) => void | Promise<void>
  ): void {
    if (mode === 'create' || mode === 'clone') {
      toast.success(t('created'));
    } else if (mode === 'edit') {
      toast.success(t('updated'));
    }
    onSuccess(conn);
  }

  async function handleSubmit(values: T): Promise<void> {
    if (mode === 'view' || !account?.id) {
      return;
    }

    if (mode === 'create' || mode === 'clone') {
      try {
        const newConnResp = await createConnection(
          createMessage(CreateConnectionRequestSchema, {
            accountId: account.id,
            name: values.connectionName,
            connectionConfig: props.buildConnectionConfig(values),
          })
        );
        if (newConnResp.connection) {
          handleOnSuccess(newConnResp.connection, mode, props.onSuccess);
        }
      } catch (err) {
        console.error(err);
        toast.error(t('createError'), {
          description: getErrorMessage(err),
        });
      }
    } else if (mode === 'edit') {
      try {
        const updatedConnResp = await updateConnection(
          createMessage(UpdateConnectionRequestSchema, {
            id: props.connection.id,
            name: values.connectionName,
            connectionConfig: props.buildConnectionConfig(values),
          })
        );
        if (updatedConnResp.connection) {
          handleOnSuccess(updatedConnResp.connection, mode, props.onSuccess);
        }
      } catch (err) {
        console.error(err);
        toast.error(t('updateError'), {
          description: getErrorMessage(err),
        });
      }
    }
  }

  async function getValueWithSecrets(): Promise<T | undefined> {
    if (mode !== 'view') {
      return undefined;
    }

    // todo: handle errors?
    const connectionResp = await getConnection({
      id: props.connection.id,
      excludeSensitive: false,
    });
    if (connectionResp.connection) {
      const formValues = props.toFormValues(connectionResp.connection);
      if (formValues) {
        return formValues;
      }
    }
    return undefined;
  }

  return {
    isLoading,
    initialValues,
    handleSubmit,
    getValueWithSecrets,
    connectionId,
  };
}
