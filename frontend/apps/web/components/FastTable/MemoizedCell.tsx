import { Cell, FlexRender, RowData } from '@tanstack/react-table';
import { memo, ReactNode } from 'react';
import { AppTableFeatures } from '../table/features';

interface Props<TData extends RowData> {
  cell: Cell<AppTableFeatures, TData>;
}

function InnerCell<TData extends RowData>(props: Props<TData>): ReactNode {
  const { cell } = props;
  return <FlexRender cell={cell} />;
}

function shouldReRender<TData extends RowData>(
  prev: Props<TData>,
  next: Props<TData>
): boolean {
  const prevValue = prev.cell.getValue();
  const nextValue = next.cell.getValue();

  if (
    // todo: maybe these should be passed in as props so they are configurable by the caller
    prev.cell.column.id === 'isSelected' ||
    prev.cell.column.id === 'actions'
  ) {
    // Always re-render checkbox cells as getIsSelected() is always the same for both
    return false;
  }

  // For other columns, just compare the values
  return prevValue === nextValue;
}

const MemoizedCell = memo(InnerCell, shouldReRender);
MemoizedCell.displayName = 'MemoizedCell';
// memo() drops the TData parameter of InnerCell, and table types are invariant
// in TData: restore the generic signature for callers.
export default MemoizedCell as typeof InnerCell;
