// A table of the job, and the WHERE clause that narrows the rows it copies.
export interface SubsetTableRow {
  schema: string;
  table: string;
  where?: string;
  isRootTable: boolean;
}
