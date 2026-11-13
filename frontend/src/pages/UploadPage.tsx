import { useCallback, useEffect, useRef, useState, type DragEvent, type FormEvent } from 'react'
import { Link, useBlocker } from 'react-router'
import { errorMessage, isAbortError } from '../api/client'
import { completeUpload, createUpload, deleteVideo, updateVideo } from '../api/endpoints'
import type { UploadPlan, Video, VideoLanguage, VideoVisibility } from '../api/types'
import { uploadMultipart } from '../api/upload'
import { ErrorBox } from '../components/ErrorBox'
import { StatusChip } from '../components/studio/StatusChip'
import { StudioHeader, UploadIcon } from '../components/studio/StudioLayout'
import { CategoryTagFields, type TagFieldHandle } from '../components/studio/CategoryTagFields'
import { ThumbnailPicker } from '../components/studio/ThumbnailPicker'
import { VisibilityFields } from '../components/studio/VisibilityFields'
import { VideoDetailsFields } from '../components/studio/VideoDetailsFields'
import { useVideoPoll } from '../hooks/useVideoPoll'
import { detailsChanges } from '../lib/categories'
import { formatDuration } from '../lib/format'
import { visibilityLabel } from '../lib/visibility'
import {
  ACCEPT_VIDEO,
  checkVideoFile,
  descriptionError,
  formatBytes,
  formatEta,
  isProcessing,
  titleError,
  titleFromFileName,
} from '../lib/upload'

/**
 * Tahap unggah:
 *   pick       belum ada file
 *   creating   POST /videos (mencatat video + minta URL presigned)
 *   uploading  PUT file langsung ke RustFS, dengan progres
 *   uploaded   file lengkap di storage, menunggu Terbitkan atau Simpan draf
 *   publishing PATCH detail (jika diubah) lalu POST /complete
 *   published  transcoding berjalan; status dicek berkala sampai READY/FAILED
 *   failed     salah satu langkah di atas gagal (lihat error.at)
 */
type Phase = 'pick' | 'creating' | 'uploading' | 'uploaded' | 'publishing' | 'published' | 'failed'

interface Progress {
  loaded: number
  total: number
  /** Perkiraan sisa waktu dalam detik, dihitung dari kecepatan rata-rata. */
  etaSeconds: number
}

interface UploadErr {
  at: 'create' | 'upload' | 'publish'
  message: string
}

/**
 * Halaman Unggah (desain Upload.dc.html). File langsung mulai diunggah
 * setelah dipilih, sambil user mengisi judul dan deskripsi, seperti di
 * desain (progres 64% sambil mengisi detail).
 *
 * Langkah 26: bahasa video, pilihan thumbnail, visibilitas, dan tombol
 * "Simpan draf". Video dicatat sebagai draf sejak file dipilih, jadi video
 * yang ditinggalkan di tengah jalan tidak pernah muncul di beranda. Panel
 * progres per kualitas di desain belum ditampilkan karena transcoder
 * belum mengirim progres per kualitas.
 */
export function UploadPage() {
  const [file, setFile] = useState<File | null>(null)
  const [fileError, setFileError] = useState<string | null>(null)
  const [video, setVideo] = useState<Video | null>(null)
  const [phase, setPhase] = useState<Phase>('pick')
  const [progress, setProgress] = useState<Progress>({ loaded: 0, total: 0, etaSeconds: Number.NaN })
  const [error, setError] = useState<UploadErr | null>(null)
  const [title, setTitle] = useState('')
  const [description, setDescription] = useState('')
  const [category, setCategory] = useState('')
  const [tags, setTags] = useState<string[]>([])
  const [language, setLanguage] = useState<VideoLanguage>('')
  const [visibility, setVisibility] = useState<VideoVisibility>('PUBLIC')
  // Tombol yang terakhir ditekan: Terbitkan (false) atau Simpan draf (true).
  const [asDraft, setAsDraft] = useState(false)
  // Pesan "Pilih kategori." baru muncul setelah Terbitkan ditekan.
  const [triedPublish, setTriedPublish] = useState(false)
  const tagsRef = useRef<TagFieldHandle>(null)
  const uploadCtrl = useRef<AbortController | null>(null)
  // Cara memotong file dari POST /videos, disimpan untuk "Coba lagi"
  // (melanjutkan bagian yang belum terkirim, langkah 29).
  const planRef = useRef<UploadPlan | null>(null)

  // Menutup halaman = membatalkan unggahan yang sedang berjalan.
  useEffect(() => () => uploadCtrl.current?.abort(), [])

  const busyTransfer = phase === 'creating' || phase === 'uploading'

  // Peringatan sebelum meninggalkan halaman saat file masih terkirim:
  // useBlocker untuk pindah halaman di dalam aplikasi, beforeunload untuk
  // menutup tab / reload. https://reactrouter.com/api/hooks/useBlocker
  // https://developer.mozilla.org/en-US/docs/Web/API/Window/beforeunload_event
  const blocker = useBlocker(busyTransfer)
  useEffect(() => {
    if (!busyTransfer) return
    const onBeforeUnload = (e: BeforeUnloadEvent) => {
      e.preventDefault()
      // Untuk browser lama (Chrome/Edge < 119), sesuai contoh di MDN.
      e.returnValue = true
    }
    window.addEventListener('beforeunload', onBeforeUnload)
    return () => window.removeEventListener('beforeunload', onBeforeUnload)
  }, [busyTransfer])

  // Setelah Terbitkan: cek status sampai transcoding selesai.
  useVideoPoll(video?.id ?? null, phase === 'published' && video !== null && isProcessing(video.status), setVideo)

  /** Mengirim bagian file yang belum tersimpan (juga untuk melanjutkan). */
  const sendParts = async (videoId: string, f: File, plan: UploadPlan, ctrl: AbortController) => {
    setError(null)
    setPhase('uploading')
    const startedAt = performance.now()
    let firstLoaded: number | null = null
    try {
      await uploadMultipart({
        videoId,
        file: f,
        plan,
        signal: ctrl.signal,
        onProgress: (loaded, total) => {
          // Kecepatan dihitung dari byte yang dikirim di sesi ini saja,
          // bukan bagian yang sudah tersimpan sebelumnya.
          firstLoaded ??= loaded
          const speed = (loaded - firstLoaded) / ((performance.now() - startedAt) / 1000)
          setProgress({ loaded, total, etaSeconds: speed > 0 ? (total - loaded) / speed : Number.NaN })
        },
      })
      if (ctrl.signal.aborted) return
      setPhase('uploaded')
    } catch (err) {
      if (isAbortError(err)) return
      setError({ at: 'upload', message: errorMessage(err) })
      setPhase('failed')
    }
  }

  const startUpload = useCallback(
    async (f: File, contentType: string, initialTitle: string, initialDescription: string) => {
      uploadCtrl.current?.abort()
      const ctrl = new AbortController()
      uploadCtrl.current = ctrl
      setError(null)
      setPhase('creating')
      setProgress({ loaded: 0, total: f.size, etaSeconds: Number.NaN })

      let created
      try {
        created = await createUpload({
          title: initialTitle,
          description: initialDescription,
          content_type: contentType,
          size_bytes: f.size,
          // Draf sampai user menekan Terbitkan (lihat komentar komponen).
          draft: true,
        })
      } catch (err) {
        if (ctrl.signal.aborted) return
        setError({ at: 'create', message: errorMessage(err) })
        setPhase('failed')
        return
      }
      if (ctrl.signal.aborted) {
        // User mengganti file saat POST /videos berjalan: buang catatan ini.
        deleteVideo(created.video.id).catch(() => {})
        return
      }
      setVideo(created.video)
      planRef.current = created.upload
      await sendParts(created.video.id, f, created.upload, ctrl)
    },
    [],
  )

  const pickFile = (f: File | undefined) => {
    if (!f) return
    const check = checkVideoFile(f)
    if (!check.ok) {
      setFileError(check.message)
      return
    }
    setFileError(null)
    setFile(f)
    setVideo(null)
    const t = titleFromFileName(f.name)
    setTitle(t)
    setDescription('')
    setCategory('')
    setTags([])
    setLanguage('')
    setVisibility('PUBLIC')
    setAsDraft(false)
    setTriedPublish(false)
    void startUpload(f, check.contentType, t, '')
  }

  /** Batalkan file sekarang dan kembali memilih file. */
  const resetFile = () => {
    uploadCtrl.current?.abort()
    // Catatan video yang filenya belum lengkap dihapus supaya tidak
    // menumpuk di Studio. Gagal pun tidak apa-apa (bisa dihapus di Studio).
    if (video && video.status === 'PENDING_UPLOAD' && phase !== 'published') {
      deleteVideo(video.id).catch(() => {})
    }
    setFile(null)
    setVideo(null)
    setError(null)
    setPhase('pick')
  }

  const retry = () => {
    if (!file) return
    if (error?.at === 'publish') {
      void publish(asDraft)
      return
    }
    // Langkah 29: unggahan yang terputus dilanjutkan dari bagian yang belum
    // tersimpan; video yang sudah dicatat tidak dibuat ulang.
    if (error?.at === 'upload' && video && video.status === 'PENDING_UPLOAD' && planRef.current) {
      uploadCtrl.current?.abort()
      const ctrl = new AbortController()
      uploadCtrl.current = ctrl
      void sendParts(video.id, file, planRef.current, ctrl)
      return
    }
    const check = checkVideoFile(file)
    if (!check.ok) return
    if (video && video.status === 'PENDING_UPLOAD') {
      deleteVideo(video.id).catch(() => {})
      setVideo(null)
    }
    const t = titleError(title) ? titleFromFileName(file.name) : title.trim()
    const d = descriptionError(description) ? '' : description.trim()
    void startUpload(file, check.contentType, t, d)
  }

  /** draft true = "Simpan draf": video tetap diproses, tapi belum terbit. */
  const publish = async (draft: boolean) => {
    if (!video) return
    setAsDraft(draft)
    setTriedPublish(true)
    // Kategori wajib dipilih saat menerbitkan (seperti YouTube Studio),
    // supaya video muncul di chip beranda. Draf boleh tanpa kategori.
    // Tag yang masih diketik ikut disimpan; null = teksnya salah.
    const finalTags = tagsRef.current ? tagsRef.current.flush() : tags
    if (titleError(title) || descriptionError(description) || (!draft && !category) || !finalTags) return
    setPhase('publishing')
    setError(null)
    try {
      let current = video
      // Video dicatat saat file dipilih (judul dari nama file), jadi detail
      // yang diisi sesudahnya dikirim sekarang lewat PATCH.
      const changes = detailsChanges(current, { title, description, category, tags: finalTags, visibility, language, draft })
      if (Object.keys(changes).length > 0) {
        current = await updateVideo(current.id, changes)
        // Disimpan sekarang: jika /complete gagal, Coba lagi tidak PATCH ulang.
        setVideo(current)
      }
      setVideo(await completeUpload(current.id))
      setPhase('published')
    } catch (err) {
      setError({ at: 'publish', message: errorMessage(err) })
      setPhase('failed')
    }
  }

  const onSubmit = (e: FormEvent) => {
    e.preventDefault()
    void publish(false)
  }

  const step = phase === 'pick' ? 1 : phase === 'published' ? 3 : 2
  const done = phase === 'published' && video?.status === 'READY'
  const fieldsLocked = phase === 'publishing' || phase === 'published'
  const canPublish = phase === 'uploaded' || (phase === 'failed' && error?.at === 'publish')

  return (
    <div className="app">
      <StudioHeader current="Unggah video" />
      <main className="upload-page">
        <ol className="steps" aria-label="Langkah unggah">
          <Step n={1} label="Pilih berkas" step={step} done={step > 1} />
          <Step n={2} label="Detail video" step={step} done={step > 2} />
          <Step n={3} label="Visibilitas & terbitkan" step={step} done={done} />
        </ol>

        {blocker.state === 'blocked' && (
          <div className="leave-box" role="alertdialog" aria-label="Tinggalkan halaman?">
            <span>Unggahan belum selesai. Jika pindah halaman, unggahan dibatalkan.</span>
            <div className="leave-actions">
              <button type="button" className="btn btn-small" onClick={() => blocker.reset()}>
                Tetap di sini
              </button>
              <button
                type="button"
                className="btn btn-small btn-danger"
                onClick={() => {
                  resetFile()
                  blocker.proceed()
                }}
              >
                Batalkan unggahan
              </button>
            </div>
          </div>
        )}

        {!file ? (
          <FilePicker onPick={pickFile} error={fileError} />
        ) : (
          <div className="upload-grid">
            <form className="panel upload-form" onSubmit={onSubmit} noValidate>
              <h1 className="panel-title">
                {phase !== 'published' ? 'Detail video' : video?.draft ? 'Draf disimpan' : 'Video diterbitkan'}
              </h1>
              {phase === 'published' && video ? (
                <PublishedNote video={video} onAnother={resetFile} onChange={setVideo} />
              ) : (
                <>
                  <VideoDetailsFields
                    idPrefix="up"
                    title={title}
                    description={description}
                    onTitleChange={setTitle}
                    onDescriptionChange={setDescription}
                    disabled={fieldsLocked}
                  />
                  <CategoryTagFields
                    idPrefix="up"
                    category={category}
                    tags={tags}
                    onCategoryChange={setCategory}
                    onTagsChange={setTags}
                    disabled={fieldsLocked}
                    emptyCategoryLabel="Pilih kategori"
                    categoryError={triedPublish && !asDraft && !category ? 'Pilih kategori.' : null}
                    tagsRef={tagsRef}
                    language={language}
                    onLanguageChange={setLanguage}
                  />
                  {video && <ThumbnailPicker video={video} onChange={setVideo} disabled={fieldsLocked} />}
                  <VisibilityFields idPrefix="up" value={visibility} onChange={setVisibility} disabled={fieldsLocked} />
                  {error && <ErrorBox message={errorText(error)} onRetry={retry} />}
                  <div className="form-actions">
                    <button type="button" className="btn btn-ghost" onClick={resetFile} disabled={phase === 'publishing'}>
                      Ganti berkas
                    </button>
                    <button type="button" className="btn" onClick={() => void publish(true)} disabled={!canPublish}>
                      {phase === 'publishing' && asDraft ? 'Menyimpan…' : 'Simpan draf'}
                    </button>
                    <button type="submit" className="btn btn-primary" disabled={!canPublish}>
                      {phase === 'publishing' && !asDraft ? 'Menerbitkan…' : 'Terbitkan'}
                    </button>
                  </div>
                  {!canPublish && phase !== 'publishing' && (
                    <p className="muted small form-hint">Tombol Simpan draf dan Terbitkan aktif setelah unggahan selesai.</p>
                  )}
                </>
              )}
            </form>

            <aside className="upload-side">
              <div className="panel">
                <FilePreview key={`${file.name}-${file.size}-${file.lastModified}`} file={file} />
                <TransferProgress phase={phase} progress={progress} errorAt={error?.at} />
              </div>
              <TranscodeCard phase={phase} video={video} />
            </aside>
          </div>
        )}
      </main>
    </div>
  )
}

/** Penjelasan di bawah status setelah Terbitkan / Simpan draf. */
function publishedHint(v: Video): string {
  if (v.draft) return 'Draf hanya terlihat olehmu. Terbitkan kapan saja dari menu Konten di Studio.'
  const where =
    v.visibility === 'PUBLIC'
      ? 'video muncul di beranda dan subscriber diberi tahu.'
      : v.visibility === 'UNLISTED'
        ? 'video bisa ditonton siapa saja yang punya tautannya.'
        : 'hanya kamu yang bisa menontonnya.'
  if (isProcessing(v.status)) return `Video sedang diubah ke format HLS. Setelah statusnya Siap, ${where}`
  if (v.status === 'READY') return `Sudah siap: ${where}`
  return ''
}

function errorText(err: UploadErr): string {
  const prefix = { create: 'Gagal memulai unggahan', upload: 'Unggahan gagal', publish: 'Gagal menerbitkan' }[err.at]
  return `${prefix}: ${err.message}`
}

function Step({ n, label, step, done }: { n: number; label: string; step: number; done: boolean }) {
  const current = n === step && !done
  const cls = done ? 'step is-done' : current ? 'step is-current' : 'step'
  return (
    <li className={cls} aria-current={current ? 'step' : undefined}>
      <span className="step-dot">{done ? '✓' : n}</span>
      {label}
    </li>
  )
}

/** Kotak pilih file. Bisa diklik atau file di-drag ke dalamnya. */
function FilePicker({ onPick, error }: { onPick: (f: File | undefined) => void; error: string | null }) {
  const [dragging, setDragging] = useState(false)
  const onDrop = (e: DragEvent<HTMLLabelElement>) => {
    e.preventDefault()
    setDragging(false)
    onPick(e.dataTransfer.files[0])
  }
  return (
    <div className="panel">
      <h1 className="panel-title">Pilih berkas</h1>
      <label
        className={dragging ? 'dropzone is-dragging' : 'dropzone'}
        onDragOver={(e) => {
          e.preventDefault()
          setDragging(true)
        }}
        onDragLeave={() => setDragging(false)}
        onDrop={onDrop}
      >
        <span className="dropzone-icon">
          <UploadIcon />
        </span>
        <strong>Seret file video ke sini, atau klik untuk memilih</strong>
        <span className="muted small">MP4, WebM, MOV, atau MKV. Maksimal 2 GB.</span>
        <input
          type="file"
          accept={ACCEPT_VIDEO}
          className="sr-only"
          onChange={(e) => {
            onPick(e.target.files?.[0])
            // Supaya memilih file yang sama lagi tetap memicu onChange.
            e.target.value = ''
          }}
        />
      </label>
      {error && <ErrorBox message={error} />}
    </div>
  )
}

interface Meta {
  width: number
  height: number
  duration: number
}

/**
 * Pratinjau file dari komputer user (belum dari server), memakai URL
 * sementara dari URL.createObjectURL. URL dibuat dan dilepas di effect yang
 * sama supaya tidak bocor memori.
 * https://developer.mozilla.org/en-US/docs/Web/API/URL/createObjectURL_static
 */
function FilePreview({ file }: { file: File }) {
  const ref = useRef<HTMLVideoElement>(null)
  const [meta, setMeta] = useState<Meta | null>(null)

  useEffect(() => {
    const el = ref.current
    if (!el) return
    const url = URL.createObjectURL(file)
    el.src = url
    return () => {
      el.removeAttribute('src')
      el.load()
      URL.revokeObjectURL(url)
    }
  }, [file])

  const details = [
    meta && meta.width > 0 ? `${meta.width}×${meta.height}` : null,
    meta && Number.isFinite(meta.duration) ? formatDuration(meta.duration * 1000) : null,
    formatBytes(file.size),
  ].filter(Boolean)

  return (
    <>
      <video
        ref={ref}
        className="upload-preview"
        muted
        playsInline
        controls
        preload="metadata"
        onLoadedMetadata={(e) => {
          const v = e.currentTarget
          setMeta({ width: v.videoWidth, height: v.videoHeight, duration: v.duration })
        }}
      />
      <div className="file-info">
        <span className="file-name">{file.name}</span>
        <span className="mono small muted">{details.join(' · ')}</span>
      </div>
    </>
  )
}

function TransferProgress({ phase, progress, errorAt }: { phase: Phase; progress: Progress; errorAt?: UploadErr['at'] }) {
  const finished = phase === 'uploaded' || phase === 'publishing' || phase === 'published' || (phase === 'failed' && errorAt === 'publish')
  const percent = finished ? 100 : progress.total > 0 ? Math.floor((progress.loaded / progress.total) * 100) : 0

  let label = 'Mengunggah'
  let detail = `${formatBytes(progress.loaded)} dari ${formatBytes(progress.total)}`
  if (phase === 'creating') {
    label = 'Menyiapkan'
    detail = 'Meminta izin unggah ke server…'
  } else if (finished) {
    label = 'Unggahan selesai'
    detail = formatBytes(progress.total)
  } else if (phase === 'failed') {
    label = 'Unggahan berhenti'
  } else {
    const eta = formatEta(progress.etaSeconds)
    if (eta) detail += ` · ${eta}`
  }

  return (
    <div className="transfer">
      <div className="transfer-head">
        <span>{label}</span>
        <span className="mono">{percent}%</span>
      </div>
      <div
        className={phase === 'failed' && !finished ? 'bar is-failed' : 'bar'}
        role="progressbar"
        aria-label="Progres unggah"
        aria-valuemin={0}
        aria-valuemax={100}
        aria-valuenow={percent}
      >
        <div className="bar-fill" style={{ width: `${percent}%` }} />
      </div>
      <span className="muted small">{detail}</span>
    </div>
  )
}

type RowState = 'wait' | 'run' | 'done' | 'fail'

/**
 * Kartu "Proses transcode (HLS)". Backend belum mengirim progres per
 * kualitas (desain: 1080p 40%, ...), jadi yang ditampilkan adalah tahapan
 * status video. Daftar kualitas muncul saat video sudah READY.
 */
function TranscodeCard({ phase, video }: { phase: Phase; video: Video | null }) {
  const status = phase === 'published' && video ? video.status : null
  const rows: { label: string; state: RowState }[] = [
    { label: 'Antre transcoding', state: !status ? 'wait' : status === 'UPLOADED' ? 'run' : 'done' },
    {
      label: 'Membuat HLS',
      state: !status || status === 'UPLOADED' ? 'wait' : status === 'PROCESSING' ? 'run' : status === 'FAILED' ? 'fail' : 'done',
    },
    { label: 'Siap ditonton', state: status === 'READY' ? 'done' : status === 'FAILED' ? 'fail' : 'wait' },
  ]
  const chipText: Record<RowState, string> = { wait: 'Menunggu', run: 'Diproses', done: 'Selesai', fail: 'Gagal' }
  const chipTone: Record<RowState, string> = { wait: 'chip', run: 'chip chip-amber', done: 'chip chip-mint', fail: 'chip chip-pink' }

  return (
    <div className="panel transcode">
      <h2 className="panel-subtitle">Proses transcode (HLS)</h2>
      {rows.map((r) => (
        <div key={r.label} className="transcode-row">
          <span>{r.label}</span>
          <span className={chipTone[r.state]}>{chipText[r.state]}</span>
        </div>
      ))}
      {video?.status === 'READY' && video.renditions && video.renditions.length > 0 && (
        <p className="mono small">Kualitas: {video.renditions.join(' · ')}</p>
      )}
      <p className="muted small">{transcodeNote(status)}</p>
    </div>
  )
}

function transcodeNote(status: Video['status'] | null): string {
  if (!status) return 'Transcoding dimulai setelah kamu menekan Terbitkan atau Simpan draf.'
  if (status === 'READY') return 'Selesai. Video sudah bisa ditonton.'
  if (status === 'FAILED') return 'Transcoding gagal. Hapus video ini di Studio lalu unggah ulang.'
  return 'Halaman ini mengecek status secara otomatis. Boleh ditutup, prosesnya tetap berjalan di server.'
}

function PublishedNote({ video, onAnother, onChange }: { video: Video; onAnother: () => void; onChange: (v: Video) => void }) {
  return (
    <div className="published">
      <p className="published-title">{video.title}</p>
      <div className="chip-row">
        <StatusChip status={video.status} />
        <span className="chip">{video.draft ? 'Draf' : visibilityLabel(video.visibility)}</span>
      </div>
      {video.status === 'FAILED' && video.failure_reason && <p className="muted small">{video.failure_reason}</p>}
      <p className="muted">{publishedHint(video)}</p>
      {video.status !== 'FAILED' && <ThumbnailPicker video={video} onChange={onChange} />}
      <div className="form-actions form-actions-start">
        {video.status === 'READY' && (
          <Link to={`/watch/${video.id}`} className="btn btn-primary">
            Tonton
          </Link>
        )}
        <Link to="/studio/content" className="btn">
          Buka Studio
        </Link>
        <button type="button" className="btn btn-ghost" onClick={onAnother}>
          Unggah video lain
        </button>
      </div>
    </div>
  )
}
