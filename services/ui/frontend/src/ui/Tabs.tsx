export type TabItem<T extends string> = {
  id: T;
  label: string;
  count?: number;
};

type TabsProps<T extends string> = {
  items: Array<TabItem<T>>;
  active: T;
  onSelect: (id: T) => void;
  label: string;
  testIdPrefix?: string;
};

// Local, in-page section switcher. Panels are rendered by the caller and
// wired with the same id pattern.
export function Tabs<T extends string>({ items, active, onSelect, label, testIdPrefix }: TabsProps<T>) {
  return (
    <div className="tabs" role="tablist" aria-label={label}>
      {items.map((item) => (
        <button
          key={item.id}
          type="button"
          role="tab"
          id={`tab-${item.id}`}
          aria-selected={item.id === active}
          aria-controls={`panel-${item.id}`}
          tabIndex={item.id === active ? 0 : -1}
          className="tab"
          data-testid={testIdPrefix ? `${testIdPrefix}-${item.id}` : undefined}
          onClick={() => onSelect(item.id)}
        >
          {item.label}
          {item.count === undefined ? null : <span className="tab-count">{item.count}</span>}
        </button>
      ))}
    </div>
  );
}

type TabPanelProps = {
  id: string;
  children: React.ReactNode;
};

export function TabPanel({ id, children }: TabPanelProps) {
  return (
    <div role="tabpanel" id={`panel-${id}`} aria-labelledby={`tab-${id}`} tabIndex={0}>
      {children}
    </div>
  );
}
