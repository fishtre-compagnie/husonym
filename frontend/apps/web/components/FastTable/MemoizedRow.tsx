import { cn } from '@/libs/utils';
import { Cell, Row } from '@tanstack/react-table';
import { VirtualItem } from '@tanstack/react-virtual';
import { memo, ReactNode } from 'react';
import { TableRow } from '../ui/table';
import { columnFlexStyle } from './columnFlexStyle';
import MemoizedCell from './MemoizedCell';

interface Props<TData> {
  row: Row<TData>;
  virtualRow: VirtualItem;
  selected: boolean;
  tableRowClassName?: string;
  disableTdWidth?: boolean;
  /**
   * Fixe la largeur de chaque cellule à la `size` de sa colonne, au lieu des
   * 187px uniformes. Doit être activé de la même façon sur l'en-tête, sinon
   * en-têtes et valeurs se décalent : avec `justify-between`, l'espace libre est
   * réparti ENTRE les cellules, donc des contenus de largeurs différentes
   * (champ de filtre en en-tête, badge en cellule) ne tombent pas au même endroit.
   */
  useColumnSizes?: boolean;
  /** Ids des colonnes qui ne s'étirent pas. Doit être identique à l'en-tête,
   *  sinon les deux se décalent. */
  noGrowColumnIds?: string[];
}

function InnerRow<TData>(props: Props<TData>): ReactNode {
  const {
    row,
    virtualRow,
    tableRowClassName,
    disableTdWidth,
    useColumnSizes,
    noGrowColumnIds,
  } = props;
  return (
    <TableRow
      key={row.id}
      style={{
        transform: `translateY(${virtualRow.start}px)`,
        height: `${virtualRow.size}px`,
      }}
      className={cn(
        'items-center flex absolute w-full px-2 gap-0 space-x-0',
        // Largeurs fixes : les cellules se suivent, sans espace réparti entre elles.
        useColumnSizes ? 'justify-start' : 'justify-between',
        tableRowClassName
      )}
    >
      {row.getVisibleCells().map((cell) => (
        <td
          key={cell.id}
          // pl-2 : compense le padding interne du champ de filtre de l'en-tête
          // (pl-2 + bordure), pour que le libellé de colonne tombe exactement
          // au-dessus des valeurs. S'applique à toutes les tables, puisque
          // l'en-tête est partagé (SchemaColumnHeader).
          className="py-2 pl-2"
          style={
            useColumnSizes
              ? columnFlexStyle(
                  cell.column.getSize(),
                  cell.column.id,
                  noGrowColumnIds
                )
              : {
                  minWidth: cell.column.getSize(),
                  width: disableTdWidth
                    ? undefined
                    : cell.column.columnDef.id === 'isSelected'
                      ? '20px'
                      : '187px',
                }
          }
        >
          {/* For some reason TS can't figure out how to type the incoming cell dynamically as Cell<TData, unknown>
              so we have to cast it here */}
          <MemoizedCell cell={cell as Cell<unknown, unknown>} />
        </td>
      ))}
    </TableRow>
  );
}

function shouldReRender<TData>(
  prev: Props<TData>,
  next: Props<TData>
): boolean {
  if (
    prev.useColumnSizes !== next.useColumnSizes ||
    prev.noGrowColumnIds !== next.noGrowColumnIds
  ) {
    return false;
  }
  if (
    prev.tableRowClassName !== next.tableRowClassName &&
    (prev.tableRowClassName !== undefined ||
      next.tableRowClassName !== undefined)
  ) {
    return false;
  }
  // Compare virtualRow properties
  if (
    prev.virtualRow.start !== next.virtualRow.start ||
    prev.virtualRow.size !== next.virtualRow.size
  ) {
    return false;
  }

  // Compare row.id
  if (prev.row.id !== next.row.id) {
    return false;
  }

  // Check row selection state for "isSelected"
  if (prev.selected !== next.selected) {
    return false;
  }

  // Check if visible cells or their values have changed
  const prevCells = prev.row.getVisibleCells();
  const nextCells = next.row.getVisibleCells();

  if (prevCells.length !== nextCells.length) {
    return false;
  }

  for (let i = 0; i < prevCells.length; i++) {
    const prevCell = prevCells[i];
    const nextCell = nextCells[i];

    if (prevCell.id !== nextCell.id) {
      return false;
    }

    // For accessor columns, compare values
    if (prevCell.getValue() !== nextCell.getValue()) {
      return false;
    }
  }

  // If no differences are found, skip re-render
  return true;
}

const MemoizedRow = memo(InnerRow, shouldReRender);
MemoizedRow.displayName = 'MemoizedRow';
export default MemoizedRow;
