/**
 * BotsApp — "Apps & Bots" admin surface (route: /apps).
 *
 * Self-hostable bot administration: list / create / edit / delete bots, rotate
 * their token + signing secret, and register slash commands + an event webhook.
 *
 * On create (and on rotate) the bot token / signing secret are shown ONCE in a
 * copyable panel with a clear "save these now" warning — they are never
 * retrievable again, matching the backend contract.
 *
 * Every network call is wrapped so a 404 (route not yet built) degrades to an
 * empty list rather than crashing the app.
 */
import { useCallback, useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import {
  Bot, Plus, Trash2, KeyRound, RefreshCw, Copy, Check, ArrowLeft,
  ShieldAlert, Webhook, Slash, X, ExternalLink, Pencil,
} from 'lucide-react'
import { api } from '../../lib/api.js'
import { toast } from '../../lib/toast.jsx'
import { Button, IconButton, Input, Modal, Card, LoadingState } from '../../components/ui'

const ALL_SCOPES = [
  { id: 'chat:write',      label: 'Send messages',   desc: 'Post messages as the bot' },
  { id: 'history:read',    label: 'Read history',    desc: 'Read channel message history' },
  { id: 'channels:read',   label: 'List channels',   desc: 'See channels in the workspace' },
  { id: 'members:read',    label: 'Read members',    desc: 'See channel members + roster' },
  { id: 'reactions:write', label: 'Add reactions',   desc: 'React to messages' },
]

async function copyText(text, label = 'Copied to clipboard') {
  try {
    await navigator.clipboard.writeText(text)
    toast.success(label)
  } catch {
    toast.error('Copy failed — copy it manually')
  }
}

// ---------------------------------------------------------------------------
// CopyRow — a labelled, monospace, copyable secret line.
// ---------------------------------------------------------------------------
function CopyRow({ label, value, Icon }) {
  const [copied, setCopied] = useState(false)
  return (
    <div>
      <p className="text-2xs uppercase tracking-eyebrow text-ink-faint mb-1 flex items-center gap-1.5">
        {Icon && <Icon size={11} />} {label}
      </p>
      <div className="flex items-center gap-2 bg-bg-sunk border border-line rounded-md px-2.5 py-1.5">
        <code className="flex-1 min-w-0 text-xs text-ink font-mono break-all">{value}</code>
        <IconButton
          size="sm"
          title={`Copy ${label}`}
          onClick={() => { copyText(value, `${label} copied`); setCopied(true); setTimeout(() => setCopied(false), 1200) }}
        >
          {copied ? <Check size={13} className="text-success" /> : <Copy size={13} />}
        </IconButton>
      </div>
    </div>
  )
}

// ---------------------------------------------------------------------------
// SlashCommandEditor — add/remove {name, description} rows.
// ---------------------------------------------------------------------------
function SlashCommandEditor({ commands, onChange }) {
  function update(i, patch) {
    onChange(commands.map((c, idx) => (idx === i ? { ...c, ...patch } : c)))
  }
  function add() { onChange([...commands, { name: '', description: '' }]) }
  function remove(i) { onChange(commands.filter((_, idx) => idx !== i)) }

  return (
    <div className="space-y-2">
      {commands.map((c, i) => (
        <div key={i} className="flex items-center gap-2">
          <div className="flex items-center bg-paper border border-line rounded-md h-9 px-2 w-32 flex-shrink-0 focus-within:border-accent">
            <Slash size={12} className="text-ink-faint flex-shrink-0" />
            <input
              value={c.name}
              onChange={(e) => update(i, { name: e.target.value.replace(/^\//, '') })}
              placeholder="deploy"
              aria-label="Command name"
              className="flex-1 min-w-0 bg-transparent outline-none text-sm text-ink pl-1 placeholder:text-ink-faint"
            />
          </div>
          <input
            value={c.description}
            onChange={(e) => update(i, { description: e.target.value })}
            placeholder="Trigger a deploy"
            aria-label="Command description"
            className="flex-1 min-w-0 bg-paper border border-line rounded-md h-9 px-3 text-sm text-ink outline-none focus:border-accent placeholder:text-ink-faint"
          />
          <IconButton size="sm" title="Remove command" onClick={() => remove(i)}>
            <X size={14} />
          </IconButton>
        </div>
      ))}
      <Button variant="ghost" size="sm" onClick={add}>
        <Plus size={13} /> Add slash command
      </Button>
    </div>
  )
}

// ---------------------------------------------------------------------------
// BotFormModal — create or edit a bot.
// ---------------------------------------------------------------------------
function BotFormModal({ open, onClose, existing, onSaved }) {
  const editing = !!existing
  const [name, setName] = useState('')
  const [scopes, setScopes] = useState(['chat:write'])
  const [eventUrl, setEventUrl] = useState('')
  const [slashCommands, setSlashCommands] = useState([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState(null)

  useEffect(() => {
    if (open) {
      setName(existing?.name || '')
      setScopes(existing?.scopes?.length ? existing.scopes : ['chat:write'])
      setEventUrl(existing?.event_url || '')
      setSlashCommands(existing?.slash_commands || [])
      setError(null)
    }
  }, [open, existing])

  function toggleScope(id) {
    setScopes((s) => (s.includes(id) ? s.filter((x) => x !== id) : [...s, id]))
  }

  async function submit(e) {
    e.preventDefault()
    if (!name.trim()) return
    setLoading(true)
    setError(null)
    const cleanCommands = slashCommands
      .map((c) => ({ name: c.name.trim(), description: c.description.trim() }))
      .filter((c) => c.name)
    try {
      if (editing) {
        const updated = await api.botUpdate(existing.id, {
          name: name.trim(), scopes, eventUrl: eventUrl.trim(), slashCommands: cleanCommands,
        })
        toast.success('Bot updated')
        onSaved({ kind: 'updated', bot: updated || { ...existing, name: name.trim(), scopes, event_url: eventUrl.trim(), slash_commands: cleanCommands } })
      } else {
        const res = await api.botCreate({
          name: name.trim(), scopes, eventUrl: eventUrl.trim(), slashCommands: cleanCommands,
        })
        toast.success('Bot created')
        onSaved({ kind: 'created', ...res })
      }
      onClose()
    } catch (err) {
      setError(err.message || 'Save failed')
    } finally {
      setLoading(false)
    }
  }

  return (
    <Modal open={open} onClose={onClose} title={editing ? 'Edit bot' : 'Create a bot'} size="lg">
      <form onSubmit={submit}>
        <Modal.Body className="space-y-5">
          {error && (
            <p role="alert" className="text-xs text-danger bg-danger-bg rounded-sm px-3 py-2">{error}</p>
          )}

          <Input
            label="Name"
            placeholder="e.g. deploy-bot"
            value={name}
            onChange={(e) => setName(e.target.value)}
            leading={<Bot size={13} />}
            autoFocus
          />

          <div>
            <p className="text-xs text-ink-muted font-medium mb-2 tracking-tightish">Scopes</p>
            <div className="grid grid-cols-1 sm:grid-cols-2 gap-1.5">
              {ALL_SCOPES.map((s) => {
                const on = scopes.includes(s.id)
                return (
                  <button
                    key={s.id}
                    type="button"
                    onClick={() => toggleScope(s.id)}
                    aria-pressed={on}
                    className={[
                      'flex items-start gap-2.5 text-left rounded-md border px-3 py-2 transition-colors duration-fast',
                      on ? 'border-accent bg-accent-tint text-ink' : 'border-line hover:border-line-strong text-ink-muted',
                    ].join(' ')}
                  >
                    <span className={[
                      'mt-0.5 flex h-4 w-4 items-center justify-center rounded-xs border flex-shrink-0',
                      on ? 'bg-accent border-accent text-white' : 'border-line-strong',
                    ].join(' ')}>
                      {on && <Check size={11} />}
                    </span>
                    <span className="min-w-0">
                      <span className="block text-sm font-medium tracking-tightish">{s.label}</span>
                      <span className="block text-2xs text-ink-faint font-mono">{s.id}</span>
                    </span>
                  </button>
                )
              })}
            </div>
          </div>

          <Input
            label="Event webhook URL (optional)"
            placeholder="https://example.com/hooks/vulos"
            value={eventUrl}
            onChange={(e) => setEventUrl(e.target.value)}
            leading={<Webhook size={13} />}
          />

          <div>
            <p className="text-xs text-ink-muted font-medium mb-2 tracking-tightish">Slash commands (optional)</p>
            <SlashCommandEditor commands={slashCommands} onChange={setSlashCommands} />
          </div>
        </Modal.Body>
        <Modal.Footer>
          <Button variant="secondary" type="button" onClick={onClose}>Cancel</Button>
          <Button variant="primary" type="submit" disabled={loading || !name.trim()}>
            {loading ? 'Saving…' : editing ? 'Save changes' : 'Create bot'}
          </Button>
        </Modal.Footer>
      </form>
    </Modal>
  )
}

// ---------------------------------------------------------------------------
// SecretsModal — show token + signing secret + webhook URL ONCE.
// ---------------------------------------------------------------------------
function SecretsModal({ open, onClose, data }) {
  if (!data) return null
  const bot = data.bot || {}
  return (
    <Modal open={open} onClose={onClose} title={`Credentials — ${bot.name || 'bot'}`} size="lg">
      <Modal.Body className="space-y-4">
        <div className="flex items-start gap-2.5 bg-warning-bg border border-line rounded-md px-3 py-2.5">
          <ShieldAlert size={16} className="text-warning flex-shrink-0 mt-0.5" />
          <p className="text-xs text-ink-muted leading-relaxed">
            <span className="font-semibold text-ink">Save these now.</span> The token and
            signing secret are shown only once and cannot be retrieved later. If you lose
            them, rotate to issue new ones.
          </p>
        </div>
        {data.token && <CopyRow label="Bot token" value={data.token} Icon={KeyRound} />}
        {data.signing_secret && <CopyRow label="Signing secret" value={data.signing_secret} Icon={KeyRound} />}
        {data.incoming_webhook_url && <CopyRow label="Incoming webhook URL" value={data.incoming_webhook_url} Icon={Webhook} />}
      </Modal.Body>
      <Modal.Footer>
        <Button variant="primary" onClick={onClose}>Done — I saved them</Button>
      </Modal.Footer>
    </Modal>
  )
}

// ---------------------------------------------------------------------------
// BotCard — one bot row with actions.
// ---------------------------------------------------------------------------
function BotCard({ bot, onEdit, onDelete, onRotateToken, onRotateSecret }) {
  return (
    <Card className="p-0">
      <div className="flex items-start gap-3 px-4 py-3">
        <span className="flex h-9 w-9 items-center justify-center rounded-md bg-accent-tint flex-shrink-0">
          <Bot size={17} className="text-accent-press" />
        </span>
        <div className="flex-1 min-w-0">
          <div className="flex items-center gap-2">
            <h3 className="text-sm font-semibold text-ink tracking-tightish truncate">{bot.name}</h3>
            {bot.incoming_webhook_url && (
              <span title="Incoming webhook configured"><Webhook size={12} className="text-ink-faint" /></span>
            )}
          </div>
          <div className="flex flex-wrap gap-1 mt-1.5">
            {(bot.scopes || []).map((s) => (
              <span key={s} className="text-2xs font-mono text-ink-muted bg-bg-elev2 border border-line rounded-pill px-1.5 py-0.5">
                {s}
              </span>
            ))}
            {(!bot.scopes || bot.scopes.length === 0) && (
              <span className="text-2xs text-ink-faint italic">no scopes</span>
            )}
          </div>
          {bot.slash_commands?.length > 0 && (
            <p className="text-2xs text-ink-faint mt-1.5">
              <Slash size={10} className="inline -mt-0.5" /> {bot.slash_commands.map((c) => `/${c.name}`).join('  ')}
            </p>
          )}
        </div>
        <div className="flex items-center gap-0.5 flex-shrink-0">
          <IconButton size="sm" title="Rotate token" onClick={() => onRotateToken(bot)}>
            <KeyRound size={14} />
          </IconButton>
          <IconButton size="sm" title="Rotate signing secret" onClick={() => onRotateSecret(bot)}>
            <RefreshCw size={14} />
          </IconButton>
          <IconButton size="sm" title="Edit bot" onClick={() => onEdit(bot)}>
            <Pencil size={14} />
          </IconButton>
          <IconButton size="sm" title="Delete bot" onClick={() => onDelete(bot)}>
            <Trash2 size={14} />
          </IconButton>
        </div>
      </div>
    </Card>
  )
}

// ---------------------------------------------------------------------------
// BotsApp — root
// ---------------------------------------------------------------------------
export default function BotsApp() {
  const navigate = useNavigate()
  const [bots, setBots] = useState([])
  const [loading, setLoading] = useState(true)
  const [showForm, setShowForm] = useState(false)
  const [editing, setEditing] = useState(null)
  const [secrets, setSecrets] = useState(null)
  const [confirmDelete, setConfirmDelete] = useState(null)

  const load = useCallback(async () => {
    try {
      const list = await api.botsList()
      setBots(list || [])
    } catch {
      // 404 (route not built yet) or network error → empty, no crash.
      setBots([])
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => { load() }, [load])

  function handleSaved(result) {
    load()
    if (result.kind === 'created') {
      setSecrets(result)
    }
  }

  async function doDelete(bot) {
    try {
      await api.botDelete(bot.id)
      toast.success('Bot deleted')
      setBots((b) => b.filter((x) => x.id !== bot.id))
    } catch (e) {
      toast.error(e.message || 'Delete failed')
    } finally {
      setConfirmDelete(null)
    }
  }

  async function rotateToken(bot) {
    try {
      const res = await api.botRotateToken(bot.id)
      setSecrets({ bot, token: res?.token })
    } catch (e) {
      toast.error(e.message || 'Rotate failed')
    }
  }

  async function rotateSecret(bot) {
    try {
      const res = await api.botRotateSecret(bot.id)
      setSecrets({ bot, signing_secret: res?.signing_secret })
    } catch (e) {
      toast.error(e.message || 'Rotate failed')
    }
  }

  return (
    <div className="flex-1 min-h-0 overflow-y-auto bg-bg">
      <header className="flex items-center gap-2 h-12 px-3 bg-paper border-b border-line sticky top-0 z-10">
        <IconButton title="Back to Talk" onClick={() => navigate('/')}>
          <ArrowLeft size={16} />
        </IconButton>
        <Bot size={16} className="text-accent-press" />
        <h1 className="text-sm font-semibold text-ink tracking-tightish">Apps &amp; Bots</h1>
        <div className="ml-auto">
          <Button variant="primary" size="sm" onClick={() => { setEditing(null); setShowForm(true) }}>
            <Plus size={14} /> Create bot
          </Button>
        </div>
      </header>

      <div className="max-w-2xl mx-auto px-4 py-6 space-y-4">
        <p className="text-xs text-ink-faint leading-relaxed">
          Build integrations that post messages, react, and respond to slash commands.
          See the{' '}
          <a
            href="/docs/BOT-API.md"
            target="_blank"
            rel="noopener noreferrer"
            className="text-accent-press underline underline-offset-2 inline-flex items-center gap-0.5"
          >
            Bots API docs <ExternalLink size={11} />
          </a>{' '}
          for the full webhook + signing reference.
        </p>

        {loading ? (
          <LoadingState label="Loading bots…" />
        ) : bots.length === 0 ? (
          <Card className="px-6 py-10 text-center">
            <Bot size={28} className="text-ink-faint mx-auto mb-3" />
            <p className="text-sm text-ink-muted tracking-tightish mb-1">No bots yet</p>
            <p className="text-xs text-ink-faint mb-4">Create your first bot to start automating your workspace.</p>
            <Button variant="primary" size="sm" onClick={() => { setEditing(null); setShowForm(true) }}>
              <Plus size={14} /> Create bot
            </Button>
          </Card>
        ) : (
          <div className="space-y-2.5">
            {bots.map((bot) => (
              <BotCard
                key={bot.id}
                bot={bot}
                onEdit={(b) => { setEditing(b); setShowForm(true) }}
                onDelete={(b) => setConfirmDelete(b)}
                onRotateToken={rotateToken}
                onRotateSecret={rotateSecret}
              />
            ))}
          </div>
        )}
      </div>

      <BotFormModal
        open={showForm}
        existing={editing}
        onClose={() => setShowForm(false)}
        onSaved={handleSaved}
      />
      <SecretsModal open={!!secrets} data={secrets} onClose={() => setSecrets(null)} />

      <Modal open={!!confirmDelete} onClose={() => setConfirmDelete(null)} title="Delete bot" size="sm">
        <Modal.Body>
          <p className="text-sm text-ink-muted">
            Delete <span className="font-semibold text-ink">{confirmDelete?.name}</span>? Its token and
            webhook stop working immediately. This cannot be undone.
          </p>
        </Modal.Body>
        <Modal.Footer>
          <Button variant="secondary" onClick={() => setConfirmDelete(null)}>Cancel</Button>
          <Button variant="destructive" onClick={() => doDelete(confirmDelete)}>Delete bot</Button>
        </Modal.Footer>
      </Modal>
    </div>
  )
}
