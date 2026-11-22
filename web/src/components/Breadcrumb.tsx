type Crumb = { id: string; name: string }

type Props = {
  stack: Crumb[]
  onNavigate: (index: number) => void
  onDropOn: (folderId: string | null) => void
}

export default function Breadcrumb({ stack, onNavigate, onDropOn }: Props) {
  return (
    <nav className="breadcrumb">
      <button
        type="button"
        className="crumb"
        onClick={() => onNavigate(-1)}
        onDragOver={(e) => e.preventDefault()}
        onDrop={(e) => {
          e.preventDefault()
          onDropOn(null)
        }}
      >
        我的文件
      </button>
      {stack.map((c, i) => (
        <span key={c.id} className="crumb-wrap">
          <span className="crumb-sep">/</span>
          <button
            type="button"
            className={i === stack.length - 1 ? 'crumb current' : 'crumb'}
            onClick={() => onNavigate(i)}
            onDragOver={(e) => e.preventDefault()}
            onDrop={(e) => {
              e.preventDefault()
              onDropOn(c.id)
            }}
          >
            {c.name}
          </button>
        </span>
      ))}
    </nav>
  )
}
