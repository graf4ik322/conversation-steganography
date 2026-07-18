import { useState, useEffect, useRef, useCallback } from "react"
import { Card } from "./components/ui/card"
import { Tabs, TabsList, TabsTrigger, TabsContent } from "./components/ui/tabs"
import { Button } from "./components/ui/button"
import { Badge } from "./components/ui/badge"
import { ScrollArea } from "./components/ui/scroll-area"
import { Textarea } from "./components/ui/textarea"
import { Input } from "./components/ui/input"
import { Slider } from "./components/ui/slider"
import { Alert, AlertTitle, AlertDescription } from "./components/ui/alert"

type ConsoleEntry = {
  t: string
  m: string
  type?: string
}

type SessionStatus = {
  alive: boolean
  ttl_seconds?: number
  remaining_seconds?: number
  ttl_mode?: "sliding" | "fixed"
}

type SetupMode = "phrase" | "invite"

const API = "/api/v1"

function generateToken(): string {
  const buf = new Uint8Array(32)
  crypto.getRandomValues(buf)
  return Array.from(buf).map((b) => b.toString(16).padStart(2, "0")).join("")
}

function getTokenFromHash(): string | null {
  const hash = window.location.hash
  if (!hash || hash === "#") return null
  return hash.slice(1)
}

function getSessionId(): string | null {
  const m = document.cookie.match(/(?:^|;\s*)steg_session_id=([^;]*)/)
  return m ? m[1] : null
}

function clearHash() {
  history.replaceState(null, "", window.location.pathname)
}

export default function App() {
  const [tab, setTab] = useState("setup")
  const [consoleEntries, setConsoleEntries] = useState<ConsoleEntry[]>([])
  const [status, setStatus] = useState<SessionStatus | null>(null)
  const [loading, setLoading] = useState(false)

  const [setupMode, setSetupMode] = useState<SetupMode>("phrase")
  const [conversationName, setConversationName] = useState("")
  const [secretPhrase, setSecretPhrase] = useState("")
  const [inviteTopic, setInviteTopic] = useState("")
  const [inviteDuration, setInviteDuration] = useState(15)
  const [inviteToken, setInviteToken] = useState<string | null>(null)
  const [inviteURL, setInviteURL] = useState("")

  const activeTokenRef = useRef<string | null>(null)
  const [plaintext, setPlaintext] = useState("")
  const [coverText, setCoverText] = useState("")
  const [decodeInput, setDecodeInput] = useState("")
  const [decodedText, setDecodedText] = useState("")
  const eventSourceRef = useRef<EventSource | null>(null)
  const consoleRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    const hashToken = getTokenFromHash()
    if (hashToken && hashToken.length >= 32) {
      activeTokenRef.current = hashToken
      setTab("encode")
      fetchStatus(hashToken)
    }
  }, [])

  const fetchStatus = useCallback(async (overrideToken?: string) => {
    const token = overrideToken || activeTokenRef.current
    const sid = getSessionId()
    if (!sid && !token) return
    const headers: Record<string, string> = {}
    if (token) headers["X-Session-Token"] = token
    try {
      const res = await fetch(`${API}/session/status`, { headers })
      const data: SessionStatus = await res.json()
      setStatus(data)
      if (!data.alive) { setConsoleEntries([]); eventSourceRef.current?.close() }
    } catch { /* ignore */ }
  }, [])

  useEffect(() => {
    if (!activeTokenRef.current) return
    const interval = setInterval(() => fetchStatus(activeTokenRef.current ?? undefined), 5000)
    return () => clearInterval(interval)
  }, [fetchStatus])

  useEffect(() => {
    if (consoleRef.current) consoleRef.current.scrollTop = consoleRef.current.scrollHeight
  }, [consoleEntries])

  const handleStartPhrase = async () => {
    if (!conversationName || !secretPhrase) return; setLoading(true)
    try {
      const res = await fetch(`${API}/session/start`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ conversation_name: conversationName, secret_phrase: secretPhrase }),
      })
      const data = await res.json()
      if (data.error) { alert(data.error); return }
      if (data.audit_events) setConsoleEntries(data.audit_events)
      setSecretPhrase(""); setTab("encode"); fetchStatus()
    } catch { alert("Failed to start session") }
    finally { setLoading(false) }
  }

  const handleCreateInvite = async () => {
    if (!inviteTopic) return; setLoading(true); const token = generateToken()
    try {
      const res = await fetch(`${API}/session/invite`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ token, topic: inviteTopic, duration_minutes: inviteDuration }),
      })
      const data = await res.json()
      if (data.error) { alert(data.error); return }
      activeTokenRef.current = token; setInviteToken(token)
      const url = `${window.location.origin}/#${token}`; setInviteURL(url)
      if (data.audit_events) setConsoleEntries(data.audit_events)
      history.pushState(null, "", `/#${token}`); setTab("encode"); fetchStatus(token)
    } catch { alert("Failed to create invite") }
    finally { setLoading(false) }
  }

  const handleEncode = async () => {
    if (!plaintext) return; setLoading(true)
    const headers: Record<string, string> = { "Content-Type": "application/json" }
    if (activeTokenRef.current) headers["X-Session-Token"] = activeTokenRef.current
    try {
      const res = await fetch(`${API}/message/encode`, { method: "POST", headers, body: JSON.stringify({ plaintext }) })
      const data = await res.json()
      if (data.error) { alert(data.error); return }
      setCoverText(data.cover_text)
      if (data.audit_events) setConsoleEntries((p) => [...p, ...data.audit_events])
      fetchStatus()
    } catch { alert("Encoding failed") }
    finally { setLoading(false) }
  }

  const handleDecode = async () => {
    if (!decodeInput) return; setLoading(true)
    const headers: Record<string, string> = { "Content-Type": "application/json" }
    if (activeTokenRef.current) headers["X-Session-Token"] = activeTokenRef.current
    try {
      const res = await fetch(`${API}/message/decode`, { method: "POST", headers, body: JSON.stringify({ cover_text: decodeInput, sender: "remote" }) })
      const data = await res.json()
      if (data.error) { alert(data.error); return }
      setDecodedText(data.plaintext)
      if (data.audit_events) setConsoleEntries((p) => [...p, ...data.audit_events])
      fetchStatus()
    } catch { alert("Decoding failed") }
    finally { setLoading(false) }
  }

  const handleRevoke = async () => {
    const headers: Record<string, string> = {}
    if (activeTokenRef.current) headers["X-Session-Token"] = activeTokenRef.current
    try {
      const res = await fetch(`${API}/session/revoke`, { method: "POST", headers })
      const data = await res.json()
      if (data.audit_events) setConsoleEntries((p) => [...p, ...data.audit_events])
    } catch { /* ignore */ }
    activeTokenRef.current = null; setInviteToken(null); setInviteURL("")
    setStatus(null); setCoverText(""); setDecodedText("")
    clearHash(); eventSourceRef.current?.close(); setTab("setup")
    setTimeout(() => setConsoleEntries([]), 100)
  }

  const isSessionAlive = status?.alive === true
  const remaining = status?.remaining_seconds ?? 0
  const isFixed = status?.ttl_mode === "fixed"
  const remainingStr = remaining > 0
    ? `${Math.floor(remaining / 60)}:${String(remaining % 60).padStart(2, "0")}`
    : "—"

  return (
    <div className="min-h-screen bg-[hsl(0,0%,98%)] flex flex-col items-center justify-center p-4 gap-4">
      <Card className="w-full max-w-xl">
        <div className="flex items-center justify-between px-6 pt-6 pb-2">
          <div>
            <h1 className="text-lg font-semibold tracking-tight">Steganography</h1>
            <p className="text-sm text-muted-foreground">Hide messages in plain sight</p>
          </div>
          {isSessionAlive && (
            <div className="flex items-center gap-3">
              <Badge variant="outline" className={`text-xs font-mono ${remaining < 60 && isFixed ? "text-red-500 border-red-300" : ""}`}>
                {remainingStr} {isFixed ? "⏳" : "↻"}
              </Badge>
              <Badge className={`text-xs ${isFixed ? "bg-amber-600 hover:bg-amber-600" : "bg-[hsl(142,76%,36%)] hover:bg-[hsl(142,76%,36%)]"} text-white`}>
                {isFixed ? "Invite" : "Active"}
              </Badge>
            </div>
          )}
        </div>

        <Tabs value={tab} onValueChange={setTab} className="px-6 pb-4">
          <TabsList className="grid grid-cols-3 mb-4">
            <TabsTrigger value="setup">Setup</TabsTrigger>
            <TabsTrigger value="encode">Encode</TabsTrigger>
            <TabsTrigger value="decode">Decode</TabsTrigger>
          </TabsList>

          <TabsContent value="setup" className="space-y-4">
            {/* shadcn Tabs as setup mode toggle */}
            <Tabs value={setupMode} onValueChange={(v) => setSetupMode(v as SetupMode)} className="w-full">
              <TabsList className="grid grid-cols-2 w-full">
                <TabsTrigger value="phrase">Secret Phrase</TabsTrigger>
                <TabsTrigger value="invite">Invite Link</TabsTrigger>
              </TabsList>
            </Tabs>

            {setupMode === "phrase" ? (
              <div className="space-y-4 pt-2">
                <div className="space-y-2">
                  <label className="text-sm font-medium">Conversation name</label>
                  <Input placeholder="e.g., secure-chat-01" value={conversationName} onChange={(e) => setConversationName(e.target.value)} />
                </div>
                <div className="space-y-2">
                  <label className="text-sm font-medium">Secret phrase (min 16 chars)</label>
                  <Input type="password" placeholder="••••••••••••••••" value={secretPhrase} onChange={(e) => setSecretPhrase(e.target.value)} />
                </div>
                <Button className="w-full" onClick={handleStartPhrase} disabled={loading || conversationName.length === 0 || secretPhrase.length < 16}>
                  {loading ? "Starting..." : "Start Secure Session"}
                </Button>
              </div>
            ) : (
              <div className="space-y-4 pt-2">
                <div className="space-y-2">
                  <label className="text-sm font-medium">Topic</label>
                  <Input placeholder="e.g., project-alpha" value={inviteTopic} onChange={(e) => setInviteTopic(e.target.value)} />
                </div>

                <div className="space-y-3">
                  <label className="text-sm font-medium">
                    Link lifetime: <span className="font-mono">{inviteDuration} min</span>
                  </label>
                  <Slider
                    value={[inviteDuration]}
                    onValueChange={([v]) => setInviteDuration(v)}
                    min={1}
                    max={120}
                    step={1}
                  />
                  <div className="flex justify-between text-[10px] text-muted-foreground px-0.5">
                    <span>1 min</span>
                    <span>60 min</span>
                    <span>120 min</span>
                  </div>
                </div>

                {!inviteURL ? (
                  <Button className="w-full" onClick={handleCreateInvite} disabled={loading || !inviteTopic}>
                    {loading ? "Creating..." : "Generate Invite Link"}
                  </Button>
                ) : (
                  <div className="space-y-4">
                    <div className="space-y-2">
                      <label className="text-sm font-medium">Invite link</label>
                      <div className="flex gap-2">
                        <code className="flex-1 p-3 rounded-lg bg-muted border text-xs font-mono break-all select-all">
                          {inviteURL}
                        </code>
                        <Button variant="secondary" onClick={() => navigator.clipboard.writeText(inviteURL)} className="shrink-0">
                          Copy
                        </Button>
                      </div>
                    </div>

                    <Alert variant="warning">
                      <AlertTitle>Security warning</AlertTitle>
                      <AlertDescription>
                        Do <em>not</em> send this invite link through the same messaging channel where you will exchange cover texts.
                        If an attacker obtains both the link and the cover texts, the encryption is bypassed.
                        Share the link through a different channel (e.g., link in Signal, chat in Telegram).
                      </AlertDescription>
                    </Alert>

                    <div className="flex gap-2">
                      <Button className="flex-1" onClick={() => setTab("encode")}>Go to Encode</Button>
                      <Button variant="outline" className="flex-1" onClick={() => { setInviteURL(""); setInviteToken(null); activeTokenRef.current = null; clearHash() }}>New Link</Button>
                    </div>
                  </div>
                )}
              </div>
            )}

            {!isSessionAlive && getSessionId() && !activeTokenRef.current && (
              <p className="text-xs text-destructive text-center">Session expired — start a new one</p>
            )}
          </TabsContent>

          <TabsContent value="encode" className="space-y-4">
            {!isSessionAlive ? (
              <div className="text-center py-8">
                <p className="text-sm text-muted-foreground mb-4">No active session</p>
                <Button variant="outline" onClick={() => setTab("setup")}>Set up session</Button>
              </div>
            ) : (
              <>
                <div className="space-y-2">
                  <label className="text-sm font-medium">Your message</label>
                  <Textarea placeholder="Type your secret message here..." value={plaintext} onChange={(e) => setPlaintext(e.target.value)} />
                </div>
                <Button className="w-full" onClick={handleEncode} disabled={loading || !plaintext}>
                  {loading ? "Generating..." : "Generate Cover Text"}
                </Button>
                {coverText && (
                  <div className="space-y-2">
                    <label className="text-sm font-medium">Cover text</label>
                    <Textarea readOnly value={coverText} />
                    <Button variant="secondary" className="w-full" onClick={() => navigator.clipboard.writeText(coverText)}>
                      Copy to Clipboard
                    </Button>
                  </div>
                )}
              </>
            )}
          </TabsContent>

          <TabsContent value="decode" className="space-y-4">
            {!isSessionAlive ? (
              <div className="text-center py-8">
                <p className="text-sm text-muted-foreground mb-4">No active session</p>
                <Button variant="outline" onClick={() => setTab("setup")}>Set up session</Button>
              </div>
            ) : (
              <>
                <div className="space-y-2">
                  <label className="text-sm font-medium">Paste cover text</label>
                  <Textarea placeholder="Paste the received cover text here..." value={decodeInput} onChange={(e) => setDecodeInput(e.target.value)} />
                </div>
                <Button className="w-full" onClick={handleDecode} disabled={loading || !decodeInput}>
                  {loading ? "Decoding..." : "Decode Message"}
                </Button>
                {decodedText && (
                  <div className="space-y-2">
                    <label className="text-sm font-medium">Decrypted message</label>
                    <div className="p-4 rounded-lg bg-green-50 border border-green-200">
                      <p className="text-sm">{decodedText}</p>
                    </div>
                  </div>
                )}
              </>
            )}
          </TabsContent>
        </Tabs>

        {isSessionAlive && (
          <div className="px-6 pb-4 flex justify-between items-center">
            <div className="flex gap-2">
              <Badge variant="outline" className="text-[10px] text-muted-foreground font-mono">
                {isFixed ? "invite" : "persistent"}
              </Badge>
              {isFixed && remaining < 300 && (
                <Badge variant="destructive" className="text-[10px] animate-pulse">
                  Expiring soon
                </Badge>
              )}
            </div>
            <button onClick={handleRevoke} className="text-xs text-destructive hover:underline">
              {isFixed ? "Destroy session" : "Revoke session"}
            </button>
          </div>
        )}
      </Card>

      {/* Transparency Console */}
      <Card className="w-full max-w-xl overflow-hidden border-0 shadow-none">
        <div className="bg-black text-[#e0e0e0] rounded-xl border border-[#222] overflow-hidden">
          <div className="flex items-center justify-between px-4 py-2 border-b border-[#222]">
            <span className="text-[10px] font-mono uppercase tracking-wider text-[#666]">Transparency Console</span>
            <span className="flex items-center gap-1.5">
              {isSessionAlive && (
                <span className={`w-1.5 h-1.5 rounded-full animate-pulse ${isFixed ? "bg-amber-500" : "bg-[hsl(142,70%,45%)]"}`} />
              )}
              <span className="text-[10px] font-mono text-[#555]">
                {isSessionAlive ? (isFixed ? "INVITE" : "LIVE") : "INACTIVE"}
              </span>
            </span>
          </div>
          <ScrollArea className="h-48">
            <div ref={consoleRef} className="p-3 font-mono text-[11px] leading-relaxed space-y-1">
              {consoleEntries.length === 0 ? (
                <p className="text-[#444] italic">Waiting for events...</p>
              ) : (
                consoleEntries.map((entry, i) => (
                  <div key={i} className="animate-in slide-in-from-bottom-1 fade-in duration-200">
                    <span className="text-[#555]">{entry.t ? entry.t.slice(11, 19) : ""}</span>{" "}
                    <span className={entry.type === "security" || entry.type === "wipe" ? "text-[hsl(142,60%,50%)]" : "text-[#ccc]"}>
                      {entry.m}
                    </span>
                  </div>
                ))
              )}
            </div>
          </ScrollArea>
        </div>
      </Card>
    </div>
  )
}
