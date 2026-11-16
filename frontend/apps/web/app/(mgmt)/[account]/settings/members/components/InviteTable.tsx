'use client';
import DateTime from '@/components/DateTime';

import {
  ColumnDef,
  ColumnFiltersState,
  ColumnVisibilityState,
  RowData,
  SortingState,
  TableFeatures,
  createColumnHelper,
  useTable,
} from '@tanstack/react-table';

import { CopyButton } from '@/components/CopyButton';
import DeleteConfirmationDialog from '@/components/DeleteConfirmationDialog';
import SkeletonTable from '@/components/skeleton/SkeletonTable';
import {
  AppTableFeatures,
  paginatedTableFeatures,
} from '@/components/table/features';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table';
import { useGetSystemAppConfig } from '@/libs/hooks/useGetSystemAppConfig';
import { useRoleControls } from '@/libs/hooks/useRoleControls';
import { getErrorMessage } from '@/util/util';
import { timestampDate } from '@bufbuild/protobuf/wkt';
import { useMutation, useQuery } from '@connectrpc/connect-query';
import { AccountRole, UserAccountService } from '@husonym/sdk';
import { TrashIcon } from '@radix-ui/react-icons';
import { useTranslations } from 'next-intl';
import { useMemo, useState } from 'react';
import { toast } from 'sonner';
import { buildInviteLink } from './InviteUserForm';
import { useAccountRoleLabel } from './SelectAccountRole';

type TableTranslator = ReturnType<
  typeof useTranslations<'settings.members.table'>
>;

interface MemberInviteRow {
  id: string;
  email: string;
  createdAt: Date;
  expiresAt: Date;
  token: string;
  role: AccountRole;
}

function getColumns(
  showRoles: boolean,
  t: TableTranslator,
  roleLabel: (role: AccountRole) => string
) {
  const columnHelper = createColumnHelper<AppTableFeatures, MemberInviteRow>();
  const emailColumn = columnHelper.accessor('email', {
    header: t('email'),
    cell: ({ getValue }) => <div>{getValue()}</div>,
  });

  const createdAtColumn = columnHelper.accessor('createdAt', {
    header: t('createdAt'),
    cell: ({ getValue }) => {
      return (
        <div className="flex space-x-2">
          <DateTime value={getValue()} />
        </div>
      );
    },
  });

  const expiresAtColumn = columnHelper.accessor('expiresAt', {
    header: t('expiresAt'),
    cell: ({ getValue }) => {
      return (
        <div className="flex space-x-2">
          <DateTime value={getValue()} />
        </div>
      );
    },
  });

  const roleColumn = columnHelper.accessor('role', {
    header: t('role'),
    cell: ({ getValue }) => (
      <span className="truncate font-medium">{roleLabel(getValue())}</span>
    ),
  });

  const actionsColumn = columnHelper.display({
    id: 'actions',
    cell: ({ row, table }) => {
      return (
        <div className="flex flex-row gap-2">
          <CopyInviteButton token={row.original.token} />
          <DeleteInviteButton
            onDeleted={() =>
              table.options.meta?.invitesTable?.onDeleted(row.original.id)
            }
            inviteId={row.original.id}
          />
        </div>
      );
    },
  });

  if (showRoles) {
    return columnHelper.columns([
      emailColumn,
      createdAtColumn,
      expiresAtColumn,
      roleColumn,
      actionsColumn,
    ]);
  }

  return columnHelper.columns([
    emailColumn,
    createdAtColumn,
    expiresAtColumn,
    actionsColumn,
  ]);
}

function useGetColumns(): ColumnDef<AppTableFeatures, MemberInviteRow>[] {
  const showRoles = useRoleControls();
  const t = useTranslations('settings.members.table');
  const roleLabel = useAccountRoleLabel();
  return useMemo(() => {
    return getColumns(showRoles, t, roleLabel);
  }, [showRoles, t, roleLabel]);
}

interface Props {
  accountId: string;
}

export function InvitesTable(props: Props): React.ReactElement {
  const { accountId } = props;
  const { data, isLoading, refetch, isFetching } = useQuery(
    UserAccountService.method.getTeamAccountInvites,
    { accountId: accountId },
    { enabled: !!accountId }
  );
  const invites = data?.invites || [];
  const invitesRows = useMemo(() => {
    return invites.map((invite): MemberInviteRow => {
      return {
        id: invite.id,
        email: invite.email,
        createdAt: invite.createdAt
          ? timestampDate(invite.createdAt)
          : new Date(),
        expiresAt: invite.expiresAt
          ? timestampDate(invite.expiresAt)
          : new Date(),
        token: invite.token,
        role: invite.role,
      };
    });
  }, [isFetching, invites]);

  const columns = useGetColumns();

  if (isLoading) {
    return <SkeletonTable />;
  }

  return (
    <DataTable
      data={invitesRows}
      columns={columns}
      onDeleted={() => refetch()}
    />
  );
}

declare module '@tanstack/react-table' {
  // eslint-disable-next-line @typescript-eslint/no-unused-vars
  interface TableMeta<TFeatures extends TableFeatures, TData extends RowData> {
    invitesTable?: {
      onDeleted(id: string): void;
    };
  }
}

interface DataTableProps {
  data: MemberInviteRow[];
  columns: ColumnDef<AppTableFeatures, MemberInviteRow>[];
  onDeleted(id: string): void;
}
function DataTable(props: DataTableProps): React.ReactElement {
  const { data, columns, onDeleted } = props;
  const [sorting, setSorting] = useState<SortingState>([]);
  const [columnFilters, setColumnFilters] = useState<ColumnFiltersState>([]);
  const [columnVisibility, setColumnVisibility] =
    useState<ColumnVisibilityState>({});
  const [rowSelection, setRowSelection] = useState({});
  const t = useTranslations('settings.members.table');

  const table = useTable({
    features: paginatedTableFeatures,
    data,
    columns,
    onSortingChange: setSorting,
    onColumnFiltersChange: setColumnFilters,
    onColumnVisibilityChange: setColumnVisibility,
    onRowSelectionChange: setRowSelection,
    state: {
      sorting,
      columnFilters,
      columnVisibility,
      rowSelection,
    },
    meta: {
      invitesTable: {
        onDeleted,
      },
    },
  });

  return (
    <div className="w-full">
      <div className="flex items-center py-4 justify-between">
        <Input
          placeholder={t('filterEmails')}
          value={(table.getColumn('email')?.getFilterValue() as string) ?? ''}
          onChange={(event) =>
            table.getColumn('email')?.setFilterValue(event.target.value)
          }
          className="max-w-sm"
        />
      </div>
      <div className="rounded-md border">
        <Table>
          <TableHeader>
            {table.getHeaderGroups().map((headerGroup) => (
              <TableRow key={headerGroup.id}>
                {headerGroup.headers.map((header) => {
                  return (
                    <TableHead key={header.id}>
                      {header.isPlaceholder ? null : (
                        <table.FlexRender header={header} />
                      )}
                    </TableHead>
                  );
                })}
              </TableRow>
            ))}
          </TableHeader>
          <TableBody>
            {table.getRowModel().rows?.length ? (
              table.getRowModel().rows.map((row) => (
                <TableRow
                  key={row.id}
                  data-state={row.getIsSelected() && 'selected'}
                >
                  {row.getVisibleCells().map((cell) => (
                    <TableCell key={cell.id}>
                      <table.FlexRender cell={cell} />
                    </TableCell>
                  ))}
                </TableRow>
              ))
            ) : (
              <TableRow>
                <TableCell
                  colSpan={columns.length}
                  className="h-24 text-center"
                >
                  {t('noResults')}
                </TableCell>
              </TableRow>
            )}
          </TableBody>
        </Table>
      </div>
    </div>
  );
}

interface DeleteInviteButtonProps {
  inviteId: string;
  onDeleted(id: string): void;
}

function DeleteInviteButton({ inviteId, onDeleted }: DeleteInviteButtonProps) {
  const { mutateAsync } = useMutation(
    UserAccountService.method.removeTeamAccountInvite
  );
  const t = useTranslations('settings.members.invites');

  async function onRemove(): Promise<void> {
    try {
      await mutateAsync({ id: inviteId });
      toast.success(t('deleted'));
      onDeleted(inviteId);
    } catch (err) {
      console.error(err);
      toast.error(t('deleteError'), {
        description: getErrorMessage(err),
      });
    }
  }

  return (
    <DeleteConfirmationDialog
      trigger={
        <Button variant="destructive" size="icon">
          <TrashIcon />
        </Button>
      }
      headerText={t('deleteHeader')}
      description=""
      onConfirm={async () => onRemove()}
    />
  );
}

interface CopyInviteButtonProps {
  token: string;
}

function CopyInviteButton({ token }: CopyInviteButtonProps) {
  const { data: systemAppData } = useGetSystemAppConfig();
  const link = buildInviteLink(systemAppData?.publicAppBaseUrl ?? '', token);
  const t = useTranslations('settings.members.invites');

  return (
    <CopyButton
      buttonVariant="outline"
      textToCopy={link}
      onCopiedText={t('copied')}
      onHoverText={t('copyLink')}
    />
  );
}
