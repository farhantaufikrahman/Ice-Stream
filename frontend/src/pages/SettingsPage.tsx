import { useEffect, useRef, useState, type ChangeEvent, type FormEvent } from 'react'
import { Link } from 'react-router'
import { errorMessage } from '../api/client'
import { removeAvatar, updateMyProfile, uploadAvatar, type ProfileChanges } from '../api/endpoints'
import type { Profile } from '../api/types'
import { useAuth } from '../auth/useAuth'
import { Avatar } from '../components/Avatar'
import { ErrorBox } from '../components/ErrorBox'
import { StudioLayout } from '../components/studio/StudioLayout'
import { ThemeToggle } from '../components/ThemeToggle'
import { formatDate } from '../lib/format'
import { ACCEPT_AVATAR, avatarFileError, bioError, displayNameError, MAX_BIO_CHARS, MAX_DISPLAY_NAME_CHARS, MIN_AVATAR_SIDE } from '../lib/profile'
import { charCount } from '../lib/upload'

/**
 * Creator Studio, menu Pengaturan (menu "Pengaturan" di desain
 * Studio.dc.html): foto profil, nama tampilan, dan bio. Tampilan isinya
 * memakai panel dan field yang sama dengan halaman Unggah.
 */
export function SettingsPage() {
  const { user } = useAuth()
  // RequireAuth sudah memastikan user login; ini hanya untuk TypeScript.
  if (!user) return null
  return (
    <StudioLayout>
      <div className="studio-top">
        <div>
          <h1 className="studio-title">Pengaturan</h1>
          <span className="muted">Profil ini tampil di halaman kanal, video, dan komentarmu.</span>
        </div>
        <div className="header-actions">
          <ThemeToggle />
          <Link to={`/channel/${encodeURIComponent(user.username)}`} className="btn">
            Lihat kanal
          </Link>
        </div>
      </div>
      <AvatarPanel user={user} />
      {/* key: form diisi ulang dari profil jika user berganti. */}
      <ProfileForm key={user.id} user={user} />
    </StudioLayout>
  )
}

/** Panel foto profil: pilih file, langsung diunggah, atau hapus. */
function AvatarPanel({ user }: { user: Profile }) {
  const { updateUser } = useAuth()
  const input = useRef<HTMLInputElement>(null)
  const [busy, setBusy] = useState<'upload' | 'remove' | null>(null)
  const [error, setError] = useState<string | null>(null)
  // Pratinjau lokal selama file diunggah, supaya user langsung melihat fotonya.
  const [preview, setPreview] = useState<string | null>(null)
  const name = user.display_name || user.username

  // Object URL memakan memori sampai dilepas, jadi lepaskan saat diganti
  // atau saat halaman ditutup.
  // https://developer.mozilla.org/en-US/docs/Web/API/URL/createObjectURL_static
  useEffect(() => {
    if (!preview) return
    return () => URL.revokeObjectURL(preview)
  }, [preview])

  const onPick = async (e: ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0]
    // Kosongkan input, supaya memilih file yang sama lagi tetap memicu onChange.
    e.target.value = ''
    if (!file) return
    const problem = avatarFileError(file)
    if (problem) {
      setError(problem)
      return
    }
    setError(null)
    setPreview(URL.createObjectURL(file))
    setBusy('upload')
    try {
      updateUser(await uploadAvatar(file))
    } catch (err) {
      setError(errorMessage(err))
    } finally {
      setPreview(null)
      setBusy(null)
    }
  }

  const onRemove = async () => {
    setError(null)
    setBusy('remove')
    try {
      updateUser(await removeAvatar())
    } catch (err) {
      setError(errorMessage(err))
    } finally {
      setBusy(null)
    }
  }

  return (
    <section className="panel settings-avatar" aria-labelledby="avatar-title">
      <h2 id="avatar-title" className="panel-subtitle">
        Foto profil
      </h2>
      <div className="settings-avatar-row">
        <span className={busy === 'upload' ? 'settings-avatar-img is-busy' : 'settings-avatar-img'}>
          {/* key: Avatar menyimpan status "gagal dimuat" per URL. */}
          <Avatar key={preview ?? user.avatar_url} name={name} url={preview ?? user.avatar_url} size={112} />
        </span>
        <div className="settings-avatar-text">
          <p className="muted small">
            JPEG, PNG, atau WebP, maksimal 2 MB, minimal {MIN_AVATAR_SIDE}×{MIN_AVATAR_SIDE} piksel. Foto dipotong persegi di bagian tengah.
          </p>
          <div className="form-actions settings-avatar-actions">
            <input ref={input} id="avatar-file" type="file" accept={ACCEPT_AVATAR} className="sr-only" onChange={onPick} disabled={busy !== null} />
            <button type="button" className="btn btn-primary" onClick={() => input.current?.click()} disabled={busy !== null}>
              {busy === 'upload' ? 'Mengunggah…' : user.avatar_url ? 'Ganti foto' : 'Pilih foto'}
            </button>
            {user.avatar_url && (
              <button type="button" className="btn" onClick={onRemove} disabled={busy !== null}>
                {busy === 'remove' ? 'Menghapus…' : 'Hapus foto'}
              </button>
            )}
          </div>
        </div>
      </div>
      {error && <ErrorBox message={error} />}
    </section>
  )
}

/** Form nama tampilan dan bio (PATCH /me/profile). */
function ProfileForm({ user }: { user: Profile }) {
  const { updateUser } = useAuth()
  const [displayName, setDisplayName] = useState(user.display_name)
  const [bio, setBio] = useState(user.bio)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [saved, setSaved] = useState(false)

  const nameErr = displayNameError(displayName)
  const bioErr = bioError(bio)
  const nameCount = charCount(displayName.trim())
  const bioCount = charCount(bio.trim())
  // Hanya field yang berubah yang dikirim (PATCH = ubah sebagian).
  const changes: ProfileChanges = {}
  if (displayName.trim() !== user.display_name) changes.display_name = displayName.trim()
  if (bio.trim() !== user.bio) changes.bio = bio.trim()
  const dirty = Object.keys(changes).length > 0

  const onSubmit = async (e: FormEvent) => {
    e.preventDefault()
    if (nameErr || bioErr || !dirty) return
    setBusy(true)
    setError(null)
    setSaved(false)
    try {
      const profile = await updateMyProfile(changes)
      updateUser(profile)
      // Tampilkan apa yang benar-benar disimpan server (sudah di-trim).
      setDisplayName(profile.display_name)
      setBio(profile.bio)
      setSaved(true)
    } catch (err) {
      setError(errorMessage(err))
    } finally {
      setBusy(false)
    }
  }

  const edit = (setter: (v: string) => void) => (e: ChangeEvent<HTMLInputElement | HTMLTextAreaElement>) => {
    setter(e.target.value)
    setSaved(false)
  }

  return (
    <section className="panel" aria-labelledby="profile-title">
      <h2 id="profile-title" className="panel-subtitle">
        Profil
      </h2>
      <form className="settings-form" onSubmit={onSubmit} noValidate>
        <div className="field">
          <div className="field-head">
            <label htmlFor="set-name">Nama tampilan</label>
            <span className={nameCount > MAX_DISPLAY_NAME_CHARS ? 'counter is-over' : 'counter'}>
              {nameCount}/{MAX_DISPLAY_NAME_CHARS}
            </span>
          </div>
          <input
            id="set-name"
            type="text"
            value={displayName}
            onChange={edit(setDisplayName)}
            disabled={busy}
            required
            autoComplete="nickname"
            aria-invalid={nameErr ? true : undefined}
            aria-describedby={nameErr ? 'set-name-err' : undefined}
          />
          {nameErr && (
            <span id="set-name-err" className="field-error">
              {nameErr}
            </span>
          )}
        </div>
        <div className="field">
          <div className="field-head">
            <label htmlFor="set-bio">Bio</label>
            <span className={bioCount > MAX_BIO_CHARS ? 'counter is-over' : 'counter'}>
              {bioCount}/{MAX_BIO_CHARS}
            </span>
          </div>
          <textarea
            id="set-bio"
            rows={5}
            value={bio}
            onChange={edit(setBio)}
            disabled={busy}
            placeholder="Ceritakan tentang kanalmu"
            aria-invalid={bioErr ? true : undefined}
            aria-describedby={bioErr ? 'set-bio-err' : undefined}
          />
          {bioErr && (
            <span id="set-bio-err" className="field-error">
              {bioErr}
            </span>
          )}
        </div>
        <dl className="settings-facts">
          <div>
            <dt>Username</dt>
            <dd>@{user.username}</dd>
          </div>
          {user.email && (
            <div>
              <dt>Email</dt>
              <dd>{user.email}</dd>
            </div>
          )}
          <div>
            <dt>Bergabung</dt>
            <dd>{formatDate(user.created_at)}</dd>
          </div>
        </dl>
        <p className="faint small">Username dan email belum bisa diubah.</p>
        {error && <ErrorBox message={error} />}
        <div className="form-actions">
          {saved && (
            <span className="accent small" role="status">
              Profil tersimpan.
            </span>
          )}
          <button type="submit" className="btn btn-primary" disabled={busy || !dirty || Boolean(nameErr || bioErr)}>
            {busy ? 'Menyimpan…' : 'Simpan'}
          </button>
        </div>
      </form>
    </section>
  )
}
