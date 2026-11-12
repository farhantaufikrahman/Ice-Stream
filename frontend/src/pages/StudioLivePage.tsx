import { useEffect, useRef, useState, type FormEvent, type ReactNode } from 'react'
import { Link } from 'react-router'
import { errorMessage } from '../api/client'
import { getMyLive, resetStreamKey, updateMyLive } from '../api/endpoints'
import type { LiveChannel, LiveRecording } from '../api/types'
import { ErrorBox } from '../components/ErrorBox'
import { HlsPlayer } from '../components/HlsPlayer'
import { StudioLayout } from '../components/studio/StudioLayout'
import { useBrowserPublisher, type BrowserPublisher } from '../hooks/useBrowserPublisher'
import { CATEGORIES } from '../lib/categories'
import { formatCount, formatDate } from '../lib/format'
import { connectionLabel, formatBitrate, limitationLabel, liveElapsed } from '../lib/live'
import { charCount } from '../lib/upload'

/** Sumber siaran (desain GoLive.dc.html). */
type Source = 'camera' | 'screen' | 'software'

const SOURCES: { id: Source; label: string }[] = [
  { id: 'camera', label: 'Kamera browser' },
  { id: 'screen', label: 'Bagikan layar' },
  { id: 'software', label: 'Aplikasi (OBS)' },
]

/** Status siaran (dan jumlah penonton) dibaca ulang dari server sesering ini. */
const REFRESH_MS = 5000
const MAX_TITLE = 100

/**
 * Creator Studio > Siaran langsung (langkah 25b, desain GoLive.dc.html).
 *
 * Tiga cara siaran:
 *   - Kamera browser / Bagikan layar: langsung dari halaman ini lewat
 *     WebRTC (WHIP), tanpa aplikasi tambahan;
 *   - Aplikasi (OBS): salin URL server dan kunci stream ke OBS.
 *
 * Status "Sedang siaran" dan jumlah penonton selalu dibaca dari server
 * (GET /me/live), jadi benar untuk ketiga cara.
 */
export function StudioLivePage() {
  const [channel, setChannel] = useState<LiveChannel | null>(null)
  const [loadError, setLoadError] = useState<string | null>(null)
  const [reloads, setReloads] = useState(0)
  const [source, setSource] = useState<Source>('camera')
  const [cameraId, setCameraId] = useState('')
  const [micId, setMicId] = useState('')
  const browserSource = source === 'software' ? 'camera' : source
  const pub = useBrowserPublisher(browserSource, source !== 'software', cameraId, micId)
  const busy = pub.state !== 'idle'

  useEffect(() => {
    const ctrl = new AbortController()
    const load = () =>
      getMyLive(ctrl.signal)
        .then((ch) => {
          setChannel(ch)
          setLoadError(null)
        })
        .catch((err) => {
          if (!ctrl.signal.aborted) setLoadError(errorMessage(err))
        })
    void load()
    const timer = setInterval(load, REFRESH_MS)
    return () => {
      ctrl.abort()
      clearInterval(timer)
    }
  }, [reloads])

  // Menutup tab saat siaran dari browser berjalan: minta konfirmasi dulu.
  useEffect(() => {
    if (pub.state !== 'live') return
    const warn = (e: BeforeUnloadEvent) => e.preventDefault()
    window.addEventListener('beforeunload', warn)
    return () => window.removeEventListener('beforeunload', warn)
  }, [pub.state])

  const current = channel?.current ?? null
  // Siaran dari OBS sedang berjalan: browser tidak boleh ikut siaran ke
  // jalur yang sama (MediaMTX menolak publisher kedua).
  const otherPublisher = current !== null && pub.state === 'idle'
  const status = current ? 'Sedang siaran' : pub.state === 'starting' ? 'Menyambung…' : 'Belum siaran'

  if (!channel && loadError) {
    return (
      <StudioLayout>
        <h1 className="studio-title">Siaran langsung</h1>
        <ErrorBox message={loadError} onRetry={() => setReloads((n) => n + 1)} />
      </StudioLayout>
    )
  }

  return (
    <StudioLayout>
      <div className="studio-top">
        <div>
          <h1 className="studio-title">Siaran langsung</h1>
          <span className="muted">Siapkan perangkat, isi detail, lalu mulai siaran.</span>
        </div>
        <div className="header-actions">
          <span className={current ? 'chip chip-pink golive-status' : 'chip golive-status'} role="status">
            {status}
          </span>
          {source !== 'software' &&
            (busy ? (
              <button type="button" className="btn" onClick={() => void pub.stop()}>
                Akhiri siaran
              </button>
            ) : (
              <button
                type="button"
                className="btn btn-live"
                onClick={() => void pub.start()}
                disabled={!channel || otherPublisher || (source === 'camera' && !pub.preview)}
                title={otherPublisher ? 'Siaran dari aplikasi lain sedang berjalan' : undefined}
              >
                Mulai siaran
              </button>
            ))}
        </div>
      </div>
      {pub.error && <ErrorBox message={pub.error} />}

      <div className="golive">
        <div className="golive-main">
          <Preview source={source} pub={pub} channel={channel} />

          <section className="panel">
            <h2 className="panel-subtitle">Sumber siaran</h2>
            <div className="golive-sources" role="tablist" aria-label="Sumber siaran">
              {SOURCES.map((s) => (
                <button
                  key={s.id}
                  type="button"
                  role="tab"
                  aria-selected={source === s.id}
                  className={source === s.id ? 'golive-source is-active' : 'golive-source'}
                  onClick={() => setSource(s.id)}
                  disabled={busy && source !== s.id}
                >
                  {s.label}
                </button>
              ))}
            </div>
            {source === 'camera' && (
              <div className="field-row">
                <DeviceSelect id="gl-cam" label="Kamera" devices={pub.cameras} value={cameraId} onChange={setCameraId} disabled={busy} fallback="Kamera bawaan" />
                <DeviceSelect id="gl-mic" label="Mikrofon" devices={pub.mics} value={micId} onChange={setMicId} disabled={busy} fallback="Mikrofon bawaan" />
              </div>
            )}
            {source === 'screen' && (
              <>
                <p className="golive-help">
                  Pilih jendela, tab, atau seluruh layar saat menekan <strong>Mulai siaran</strong>. Suara diambil dari mikrofon.
                </p>
                <DeviceSelect id="gl-mic-screen" label="Mikrofon" devices={pub.mics} value={micId} onChange={setMicId} disabled={busy} fallback="Mikrofon bawaan" />
              </>
            )}
            {source === 'software' && channel && <SoftwareSetup channel={channel} onChange={setChannel} />}
            {otherPublisher && source !== 'software' && (
              <p className="golive-help">Siaran dari aplikasi lain (misalnya OBS) sedang berjalan. Akhiri dulu di aplikasi itu untuk siaran dari browser.</p>
            )}
          </section>
        </div>

        <aside className="golive-side">
          {channel && <DetailsForm channel={channel} onSaved={setChannel} />}
          <Health source={source} pub={pub} channel={channel} />
        </aside>
      </div>
    </StudioLayout>
  )
}

function Preview({ source, pub, channel }: { source: Source; pub: BrowserPublisher; channel: LiveChannel | null }) {
  const videoRef = useRef<HTMLVideoElement>(null)
  const current = channel?.current ?? null
  const showLocal = source !== 'software' && pub.preview !== null

  // srcObject tidak bisa diisi lewat atribut JSX.
  useEffect(() => {
    if (videoRef.current) videoRef.current.srcObject = showLocal ? pub.preview : null
  }, [showLocal, pub.preview])

  if (source === 'software' && current?.playback_url) {
    // Pratinjau siaran OBS: yang dilihat penonton (HLS), jeda beberapa detik.
    return (
      <div className="player-card">
        <HlsPlayer src={current.playback_url} />
      </div>
    )
  }

  const placeholder =
    source === 'software'
      ? 'Menunggu sinyal dari aplikasi streaming'
      : source === 'screen'
        ? 'Belum ada layar yang dipilih'
        : (pub.mediaError ?? 'Membuka kamera…')

  return (
    <div className="golive-preview">
      {showLocal ? (
        <video ref={videoRef} autoPlay muted playsInline className={source === 'camera' ? 'is-mirrored' : undefined} />
      ) : (
        <p className="golive-placeholder">{placeholder}</p>
      )}
      <span className="live-pill golive-tag">Pratinjau</span>
      {current && (
        <span className="live-badge golive-live">
          LIVE · <Elapsed startedAt={current.started_at} />
        </span>
      )}
      {showLocal && (
        <>
          <div className="golive-toggles">
            <button type="button" className="golive-round" onClick={pub.toggleMic} aria-pressed={!pub.micOn} aria-label={pub.micOn ? 'Matikan mikrofon' : 'Nyalakan mikrofon'} title={pub.micOn ? 'Matikan mikrofon' : 'Nyalakan mikrofon'}>
              <MicIcon off={!pub.micOn} />
            </button>
            {source === 'camera' && (
              <button type="button" className="golive-round" onClick={pub.toggleCam} aria-pressed={!pub.camOn} aria-label={pub.camOn ? 'Matikan kamera' : 'Nyalakan kamera'} title={pub.camOn ? 'Matikan kamera' : 'Nyalakan kamera'}>
                <CamIcon off={!pub.camOn} />
              </button>
            )}
          </div>
          <LevelMeter level={pub.micOn ? pub.level : 0} />
        </>
      )}
    </div>
  )
}

function Elapsed({ startedAt }: { startedAt: string }) {
  const [now, setNow] = useState(() => Date.now())
  useEffect(() => {
    const timer = setInterval(() => setNow(Date.now()), 1000)
    return () => clearInterval(timer)
  }, [])
  return <span className="mono">{liveElapsed(startedAt, now)}</span>
}

/** Lima batang level suara (desain GoLive.dc.html). */
function LevelMeter({ level }: { level: number }) {
  const lit = Math.round(level * 5)
  return (
    <div className="golive-level" role="meter" aria-label="Level suara mikrofon" aria-valuemin={0} aria-valuemax={5} aria-valuenow={lit}>
      {[8, 14, 20, 14, 8].map((h, i) => (
        <span key={i} style={{ height: h }} className={i < lit ? 'is-lit' : undefined} />
      ))}
    </div>
  )
}

function DeviceSelect(props: {
  id: string
  label: string
  devices: MediaDeviceInfo[]
  value: string
  onChange: (v: string) => void
  disabled: boolean
  fallback: string
}) {
  return (
    <div className="field">
      <label htmlFor={props.id}>{props.label}</label>
      <select id={props.id} value={props.value} onChange={(e) => props.onChange(e.target.value)} disabled={props.disabled}>
        <option value="">{props.fallback}</option>
        {props.devices.map((d, i) => (
          <option key={d.deviceId} value={d.deviceId}>
            {d.label || `${props.label} ${i + 1}`}
          </option>
        ))}
      </select>
    </div>
  )
}

/** URL server dan kunci stream untuk OBS. */
function SoftwareSetup({ channel, onChange }: { channel: LiveChannel; onChange: (ch: LiveChannel) => void }) {
  const [key, setKey] = useState<string | null>(null)
  const [showKey, setShowKey] = useState(false)
  const [confirming, setConfirming] = useState(false)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const createKey = async () => {
    setBusy(true)
    setError(null)
    try {
      const res = await resetStreamKey()
      setKey(res.stream_key)
      setShowKey(false)
      setConfirming(false)
      onChange(res.channel)
    } catch (err) {
      setError(errorMessage(err))
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="golive-software">
      <p className="golive-help">Pakai OBS atau aplikasi streaming lain. Salin dua nilai ini ke Pengaturan → Stream (Layanan: Custom).</p>
      <CopyField id="gl-url" label="URL server (RTMP)" value={channel.ingest_url} />
      {key ? (
        <>
          <CopyField id="gl-key" label="Kunci stream" value={key} hidden={!showKey} extra={
            <button type="button" className="btn" onClick={() => setShowKey((v) => !v)}>
              {showKey ? 'Sembunyikan' : 'Tampilkan'}
            </button>
          } />
          <p className="field-error">Jangan bagikan kunci ini. Siapa pun yang memilikinya bisa siaran di channel kamu. Kunci hanya ditampilkan sekali ini.</p>
        </>
      ) : (
        <div className="field">
          <span className="field-label">Kunci stream</span>
          <p className="golive-help">
            {channel.has_stream_key && channel.stream_key_created_at
              ? `Kunci dibuat ${formatDate(channel.stream_key_created_at)}. Demi keamanan, kunci hanya ditampilkan sekali saat dibuat. Lupa? Buat kunci baru.`
              : 'Kamu belum punya kunci stream.'}
          </p>
        </div>
      )}
      {confirming ? (
        <div className="golive-confirm" role="alert">
          <span>Kunci lama langsung tidak berlaku. Lanjutkan?</span>
          <button type="button" className="btn btn-danger" onClick={() => void createKey()} disabled={busy}>
            Ya, buat kunci baru
          </button>
          <button type="button" className="btn" onClick={() => setConfirming(false)} disabled={busy}>
            Batal
          </button>
        </div>
      ) : (
        <div className="form-actions form-actions-start">
          <button type="button" className="btn" onClick={() => (channel.has_stream_key ? setConfirming(true) : void createKey())} disabled={busy}>
            {channel.has_stream_key ? 'Buat kunci baru' : 'Buat kunci stream'}
          </button>
        </div>
      )}
      {error && <ErrorBox message={error} />}
    </div>
  )
}

function CopyField({ id, label, value, hidden = false, extra }: { id: string; label: string; value: string; hidden?: boolean; extra?: ReactNode }) {
  const inputRef = useRef<HTMLInputElement>(null)
  const [copied, setCopied] = useState(false)
  const copy = async () => {
    try {
      await navigator.clipboard.writeText(value)
      setCopied(true)
      setTimeout(() => setCopied(false), 2000)
    } catch {
      // Clipboard ditolak: pilih teksnya supaya bisa disalin manual.
      if (inputRef.current) {
        inputRef.current.type = 'text'
        inputRef.current.select()
      }
    }
  }
  return (
    <div className="field">
      <label htmlFor={id}>{label}</label>
      <div className="share-row">
        <input id={id} ref={inputRef} className="share-url mono" type={hidden ? 'password' : 'text'} value={value} readOnly autoComplete="off" />
        {extra}
        <button type="button" className="btn" onClick={() => void copy()}>
          {copied ? 'Tersalin' : 'Salin'}
        </button>
      </div>
    </div>
  )
}

/** Tiga pilihan siaran di desain GoLive ("Detail siaran"), langkah 27. */
type LiveOptions = Pick<LiveChannel, 'record_enabled' | 'slow_mode' | 'followers_only'>

const OPTIONS: { key: keyof LiveOptions; id: string; label: string }[] = [
  { key: 'record_enabled', id: 'gl-record', label: 'Simpan rekaman sebagai video setelah siaran selesai' },
  { key: 'slow_mode', id: 'gl-slow', label: 'Mode lambat chat (3 detik per pesan)' },
  { key: 'followers_only', id: 'gl-follow', label: 'Chat hanya untuk pengikut' },
]

function DetailsForm({ channel, onSaved }: { channel: LiveChannel; onSaved: (ch: LiveChannel) => void }) {
  const [title, setTitle] = useState(channel.title)
  const [category, setCategory] = useState(channel.category)
  const [options, setOptions] = useState<LiveOptions>({
    record_enabled: channel.record_enabled,
    slow_mode: channel.slow_mode,
    followers_only: channel.followers_only,
  })
  const [busy, setBusy] = useState(false)
  const [msg, setMsg] = useState<{ ok: boolean; text: string } | null>(null)
  const count = charCount(title.trim())
  const invalid = count === 0 || count > MAX_TITLE
  const dirty =
    title.trim() !== channel.title || category !== channel.category || OPTIONS.some((o) => options[o.key] !== channel[o.key])

  const onSubmit = async (e: FormEvent) => {
    e.preventDefault()
    if (invalid) return
    setBusy(true)
    setMsg(null)
    try {
      const ch = await updateMyLive({ title, category, ...options })
      setTitle(ch.title)
      setCategory(ch.category)
      setOptions({ record_enabled: ch.record_enabled, slow_mode: ch.slow_mode, followers_only: ch.followers_only })
      onSaved(ch)
      setMsg({ ok: true, text: ch.current ? 'Tersimpan. Judul dan pilihan chat langsung berlaku untuk penonton.' : 'Tersimpan.' })
    } catch (err) {
      setMsg({ ok: false, text: errorMessage(err) })
    } finally {
      setBusy(false)
    }
  }

  return (
    <form className="panel" onSubmit={onSubmit}>
      <h2 className="panel-subtitle">Detail siaran</h2>
      <div className="field">
        <div className="field-head">
          <label htmlFor="gl-title">Judul</label>
          <span className={count > MAX_TITLE ? 'counter is-over' : 'counter'}>
            {count}/{MAX_TITLE}
          </span>
        </div>
        <input id="gl-title" type="text" value={title} onChange={(e) => setTitle(e.target.value)} aria-invalid={invalid || undefined} />
      </div>
      <div className="field">
        <label htmlFor="gl-cat">Kategori</label>
        <select id="gl-cat" value={category} onChange={(e) => setCategory(e.target.value)}>
          <option value="">Tanpa kategori</option>
          {CATEGORIES.map((c) => (
            <option key={c.value} value={c.value}>
              {c.label}
            </option>
          ))}
        </select>
      </div>
      {OPTIONS.map((o) => (
        <label key={o.key} htmlFor={o.id} className="check-row golive-option">
          <input
            id={o.id}
            type="checkbox"
            checked={options[o.key]}
            onChange={(e) => setOptions((prev) => ({ ...prev, [o.key]: e.target.checked }))}
          />
          {o.label}
        </label>
      ))}
      {channel.last_recording && <RecordingStatus recording={channel.last_recording} />}
      {msg && (
        <p className={msg.ok ? 'muted small' : 'field-error'} role="status">
          {msg.text}
        </p>
      )}
      <div className="form-actions">
        <button type="submit" className="btn btn-primary" disabled={busy || invalid || !dirty}>
          {busy ? 'Menyimpan…' : 'Simpan'}
        </button>
      </div>
    </form>
  )
}

/**
 * Rekaman siaran terakhir (langkah 27). Rekaman diproses beberapa detik
 * setelah siaran selesai, lalu menjadi video DRAF yang bisa diedit dan
 * diterbitkan dari menu Konten.
 */
function RecordingStatus({ recording }: { recording: LiveRecording }) {
  const when = recording.ended_at ? ` (${formatDate(recording.ended_at)})` : ''
  let text: ReactNode
  switch (recording.status) {
    case 'PENDING':
    case 'IMPORTING':
      text = `Rekaman siaran terakhir${when} sedang disimpan menjadi video…`
      break
    case 'DONE':
      text = (
        <>
          Rekaman siaran terakhir{when} sudah menjadi video draf. <Link to="/studio/content">Buka di Konten</Link> untuk mengedit dan
          menerbitkannya.
        </>
      )
      break
    case 'FAILED':
      text = `Rekaman siaran terakhir${when} gagal disimpan: ${recording.error || 'alasan tidak diketahui'}.`
      break
  }
  return (
    <p className={recording.status === 'FAILED' ? 'field-error' : 'muted small'} role="status">
      {text}
    </p>
  )
}

function Health({ source, pub, channel }: { source: Source; pub: BrowserPublisher; channel: LiveChannel | null }) {
  const current = channel?.current ?? null
  const fromBrowser = source !== 'software' && pub.state !== 'idle'
  const rows: { label: string; value: string; ok: boolean }[] = fromBrowser
    ? [
        { label: 'Koneksi', value: connectionLabel(pub.connection), ok: pub.connection === 'connected' },
        { label: 'Bitrate kirim', value: pub.stats ? formatBitrate(pub.stats.bitrate) : '—', ok: Boolean(pub.stats?.bitrate) },
        {
          label: 'Resolusi',
          value: pub.stats?.width ? `${pub.stats.width}×${pub.stats.height} · ${pub.stats.fps} fps` : '—',
          ok: Boolean(pub.stats?.width),
        },
      ]
    : [{ label: 'Sinyal', value: current ? 'Diterima server' : '—', ok: current !== null }]
  const warning = fromBrowser && pub.stats ? limitationLabel(pub.stats.limitation) : null

  return (
    <section className="panel">
      <h2 className="panel-subtitle">Kesehatan siaran</h2>
      <dl className="golive-health">
        {rows.map((r) => (
          <div key={r.label}>
            <dt>{r.label}</dt>
            <dd className={r.ok ? 'mono is-ok' : 'mono'}>{r.value}</dd>
          </div>
        ))}
        <div>
          <dt>Penonton</dt>
          <dd className="mono">{current ? formatCount(current.viewer_count) : '0'}</dd>
        </div>
        {current && (
          <div>
            <dt>Puncak penonton</dt>
            <dd className="mono">{formatCount(current.peak_viewers)}</dd>
          </div>
        )}
      </dl>
      {warning && <p className="field-error">{warning}</p>}
      {current ? (
        <Link to={`/live/${current.id}`} className="small" target="_blank" rel="noopener">
          Lihat halaman live sebagai penonton
        </Link>
      ) : (
        <span className="muted small">Halaman penonton muncul beberapa detik setelah siaran dimulai.</span>
      )}
    </section>
  )
}

function MicIcon({ off }: { off: boolean }) {
  return (
    <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <rect x="9" y="2" width="6" height="12" rx="3" />
      <path d="M5 10v1a7 7 0 0 0 14 0v-1" />
      <line x1="12" y1="18" x2="12" y2="22" />
      {off && <line x1="3" y1="3" x2="21" y2="21" />}
    </svg>
  )
}

function CamIcon({ off }: { off: boolean }) {
  return (
    <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <polygon points="23 7 16 12 23 17 23 7" />
      <rect x="1" y="5" width="15" height="14" rx="2" />
      {off && <line x1="2" y1="2" x2="22" y2="22" />}
    </svg>
  )
}
