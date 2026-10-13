export function InboxSkeleton() {
  return <div className="inbox-skeleton" role="status" aria-label="Loading pull requests">
    <span className="sr-only">Loading pull requests…</span>
    <div aria-hidden="true">{Array.from({length: 6}, (_, row) => <div className="skeleton-inbox-row" key={row}>
      <span className="skeleton skeleton-icon" />
      <div><span className="skeleton skeleton-line" style={{width: `${84 - row % 3 * 9}%`}} />
        <span className="skeleton skeleton-line short" /><span className="skeleton skeleton-line tiny" /></div>
    </div>)}</div>
  </div>
}

export function ReviewSkeleton({label = 'Loading pull request changes…'}: {label?: string}) {
  return <div className="review-skeleton" role="status" aria-label={label}>
    <span className="loading-caption">{label}</span>
    <div className="skeleton-review-toolbar" aria-hidden="true"><span className="skeleton skeleton-line" /><span className="skeleton skeleton-button" /></div>
    <div className="skeleton-diff-layout" aria-hidden="true">
      <div className="skeleton-file-tree">{Array.from({length: 7}, (_, row) => <span className="skeleton skeleton-line" key={row} style={{width:`${88 - row % 3 * 12}%`}} />)}</div>
      <div className="skeleton-code">{Array.from({length: 18}, (_, row) => <div key={row}><span className="skeleton skeleton-line-number" /><span className="skeleton skeleton-line" style={{width:`${32 + row * 17 % 55}%`}} /></div>)}</div>
    </div>
  </div>
}

export function RepositorySkeleton({name}: {name: string}) {
  return <section className="repository-skeleton" aria-busy="true"><div className="skeleton-repository-heading"><span className="skeleton skeleton-icon" aria-hidden="true"/><span>Opening {name}…</span></div><div className="repository-skeleton-panes"><InboxSkeleton/><ReviewSkeleton label="Reading repository…"/></div></section>
}
