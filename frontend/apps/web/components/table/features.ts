import {
  columnFacetingFeature,
  columnFilteringFeature,
  columnSizingFeature,
  columnVisibilityFeature,
  createFacetedMinMaxValues,
  createFacetedRowModel,
  createFacetedUniqueValues,
  createFilteredRowModel,
  createPaginatedRowModel,
  createSortedRowModel,
  filterFn_arrIncludes,
  filterFn_equals,
  filterFn_includesString,
  filterFn_inNumberRange,
  filterFn_weakEquals,
  rowPaginationFeature,
  rowSelectionFeature,
  rowSortingFeature,
  sortFn_alphanumeric,
  sortFn_datetime,
  sortFn_text,
  tableFeatures,
} from '@tanstack/react-table';

// Every table of the app registers the same features. TanStack Table types are
// invariant in their feature set, so a single set is what lets the shared
// components (column headers, FastTable, pagination) accept any of the tables.
// Unused features cost nothing: row models are only computed when read.
const features = {
  columnFilteringFeature,
  columnFacetingFeature,
  columnSizingFeature,
  columnVisibilityFeature,
  rowPaginationFeature,
  rowSelectionFeature,
  rowSortingFeature,
  filteredRowModel: createFilteredRowModel(),
  sortedRowModel: createSortedRowModel(),
  facetedRowModel: createFacetedRowModel(),
  facetedUniqueValues: createFacetedUniqueValues(),
  facetedMinMaxValues: createFacetedMinMaxValues(),
  // `'auto'` only resolves registered functions: register those it resolved to
  // in v8 (`includesString` is also named by columns), so that filtering and
  // sorting behave as before.
  filterFns: {
    includesString: filterFn_includesString,
    inNumberRange: filterFn_inNumberRange,
    equals: filterFn_equals,
    arrIncludes: filterFn_arrIncludes,
    weakEquals: filterFn_weakEquals,
  },
  sortFns: {
    alphanumeric: sortFn_alphanumeric,
    text: sortFn_text,
    datetime: sortFn_datetime,
  },
};

type PaginatedRowModel = ReturnType<typeof createPaginatedRowModel>;

// The paginated row model is optional in the type so that paginated and
// unpaginated tables still share it.
export type AppTableFeatures = typeof features & {
  paginatedRowModel?: PaginatedRowModel;
};

// `table.getRowModel()` returns every row: for virtualized or unpaginated tables.
export const unpaginatedTableFeatures: AppTableFeatures =
  tableFeatures(features);

// `table.getRowModel()` returns the current page only.
export const paginatedTableFeatures: AppTableFeatures = tableFeatures({
  ...features,
  paginatedRowModel: createPaginatedRowModel(),
});
