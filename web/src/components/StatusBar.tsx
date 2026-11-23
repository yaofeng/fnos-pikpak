// StatusBar is the footer every file page shows: how many entries are listed,
// how many are selected, and the last operation's result or error. One copy so
// the wording cannot drift between 我的文件, the favourites and a share.
export default function StatusBar({
  selected,
  total,
  notice,
  error,
}: {
  selected: number
  total: number
  notice?: string
  error?: string
}) {
  return (
    <div className="status-bar">
      <span>{selected > 0 ? `选中 ${selected} 项（共 ${total} 项）` : `共 ${total} 项`}</span>
      {notice && <span className="ok status-note">{notice}</span>}
      {error && <span className="error status-note">{error}</span>}
    </div>
  )
}
