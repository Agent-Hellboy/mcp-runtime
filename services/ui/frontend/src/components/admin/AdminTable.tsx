import type { ReactNode } from "react";

export type AdminColumn<T> = {
  id: string;
  header: string;
  cell: (row: T) => ReactNode;
  // Marks the column that identifies the row; rendered as a row header.
  rowHeader?: boolean;
};

type AdminTableProps<T> = {
  caption: string;
  columns: Array<AdminColumn<T>>;
  rows: T[];
  rowKey: (row: T) => string;
  emptyMessage: string;
  testId: string;
};

// Small semantic table shared by the admin panels. Wide content scrolls inside
// its own bounded region so the page never scrolls horizontally.
export function AdminTable<T>({
  caption,
  columns,
  rows,
  rowKey,
  emptyMessage,
  testId,
}: AdminTableProps<T>) {
  return (
    <div className="table-scroll" data-testid={`${testId}-scroll`} tabIndex={0}>
      <table className="data-table" data-testid={testId}>
        <caption className="visually-hidden">{caption}</caption>
        <thead>
          <tr>
            {columns.map((column) => (
              <th key={column.id} scope="col">
                {column.header}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {rows.length === 0 ? (
            <tr>
              <td
                colSpan={columns.length}
                className="table-empty"
                data-testid={`${testId}-empty`}
              >
                {emptyMessage}
              </td>
            </tr>
          ) : (
            rows.map((row) => (
              <tr key={rowKey(row)} data-testid={`${testId}-row`}>
                {columns.map((column) =>
                  column.rowHeader ? (
                    <th key={column.id} scope="row">
                      {column.cell(row)}
                    </th>
                  ) : (
                    <td key={column.id}>{column.cell(row)}</td>
                  )
                )}
              </tr>
            ))
          )}
        </tbody>
      </table>
    </div>
  );
}
