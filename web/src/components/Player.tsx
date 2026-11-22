import { useEffect, useRef, useState } from 'react'
import { apiGet, basePath } from '../api'
import { formatClock, guessMime } from '../player'

type PlayInfo = {
  file_id: string
  name: string
  mime: string
  size: number
  stream_url: string
}

const rates = [0.5, 1, 1.25, 1.5, 2]

export default function Player({
  fileId,
  name,
  onClose,
  infoPath,
}: {
  fileId: string
  name: string
  onClose: () => void
  // infoPath overrides where the stream info comes from, so a file inside a
  // share can play through the same player.
  infoPath?: string
}) {
  const [info, setInfo] = useState<PlayInfo | null>(null)
  const [error, setError] = useState('')
  const [rate, setRate] = useState(1)
  const [unsupported, setUnsupported] = useState(false)
  const [time, setTime] = useState(0)
  const [duration, setDuration] = useState(0)
  const videoRef = useRef<HTMLVideoElement | null>(null)
  const shellRef = useRef<HTMLDivElement | null>(null)

  useEffect(() => {
    let alive = true
    setInfo(null)
    setError('')
    apiGet<PlayInfo>(infoPath ?? `play/${fileId}`)
      .then((got) => {
        if (alive) setInfo(got)
      })
      .catch((e: Error) => {
        if (alive) setError(e.message)
      })
    return () => {
      alive = false
    }
  }, [fileId, infoPath])

  const mime = info ? guessMime(info.name, info.mime) : ''
  const src = info ? `${basePath}${info.stream_url}` : ''

  // No transcoding in v1: if the browser cannot decode the file, say so and
  // point at the download instead of spinning forever.
  useEffect(() => {
    const v = videoRef.current
    if (!v || !info) return
    setUnsupported(v.canPlayType(mime) === '')
  }, [info, mime])

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        if (document.fullscreenElement) void document.exitFullscreen()
        else onClose()
      }
    }
    document.addEventListener('keydown', onKey)
    return () => document.removeEventListener('keydown', onKey)
  }, [onClose])

  function changeRate(r: number) {
    setRate(r)
    if (videoRef.current) videoRef.current.playbackRate = r
  }

  function toggleFullscreen() {
    if (document.fullscreenElement) {
      void document.exitFullscreen()
      return
    }
    void shellRef.current?.requestFullscreen()
  }

  return (
    <div className="modal-backdrop player-backdrop" onClick={onClose}>
      <div className="player" onClick={(e) => e.stopPropagation()} ref={shellRef}>
        <header className="player-head">
          <strong className="player-title" title={info?.name || name}>
            {info?.name || name}
          </strong>
          <div className="player-actions">
            {rates.map((r) => (
              <button key={r} type="button" className={rate === r ? 'active' : ''} onClick={() => changeRate(r)}>
                {r}×
              </button>
            ))}
            <button type="button" onClick={toggleFullscreen}>
              全屏
            </button>
            {src && (
              <a className="button" href={src} download={info?.name || name}>
                下载
              </a>
            )}
            <button type="button" onClick={onClose}>
              关闭
            </button>
          </div>
        </header>

        {error && <p className="error">{error}</p>}
        {!info && !error && <p className="muted">正在获取播放地址…</p>}
        {unsupported && (
          <p className="notice">浏览器不支持该编码（{mime}），可下载后使用本地播放器观看。</p>
        )}

        {src && (
          <video
            ref={videoRef}
            className="player-video"
            src={src}
            controls
            autoPlay
            playsInline
            onTimeUpdate={(e) => setTime(e.currentTarget.currentTime)}
            onLoadedMetadata={(e) => setDuration(e.currentTarget.duration || 0)}
            onError={() => setUnsupported(true)}
          />
        )}

        <footer className="player-foot muted small">
          <span>
            {formatClock(time)} / {formatClock(duration)}
          </span>
          {info && <span>{mime}</span>}
        </footer>
      </div>
    </div>
  )
}
