import { useState, useEffect, useRef, useCallback } from "react"
import { Card } from "./components/ui/card"
import { Tabs, TabsList, TabsTrigger, TabsContent } from "./components/ui/tabs"
import { Button } from "./components/ui/button"
import { Badge } from "./components/ui/badge"
import { ScrollArea } from "./components/ui/scroll-area"
import { Textarea } from "./components/ui/textarea"

type ConsoleEntry = {
  t: string
  m: string
  type?: string
}

type SessionStatus = {
  alive: boolean
  ttl_seconds?: number
  remaining_seconds?: number
}

const API = "/api/v1"

function getSessionId(): string | null {
  const match = document.cookie.match(/(?:^|;\s*)steg_session_id=([^;]*)/)
  return match ? match[1] : null
}

export default function App() {
  const [tab, setTab] = useState("setup")
  const [sessionId, setSessionId] = useState<string | null>(getSessionId)
  const [status, setStatus] = useState<SessionStatus | null>(null)
  const [consoleEntries, setConsoleEntries] = useState<ConsoleEntry[]>([])
  const [conversationName, setConversationName] = useState("")
  const [secretPhrase, setSecretPhrase] = useState("")
  const [plaintext, setPlaintext] = useState("")
  const [coverText, setCoverText] = useState("")
  const [decodeInput, setDecodeInput] = useState("")
  const [decodedText, setDecodedText] = useState("")
  const [loading, setLoading] = useState(false)
  const consoleRef = useRef<HTMLDivElement>(null)
  const eventSourceRef = useRef<EventSource | null>(null)

  // Fetch session status
  const fetchStatus = useCallback(async () => {
    const sid = getSessionId()
    if (!sid) return
    try {
      const res = await fetch(`${API}/session/status`)
      const data = await res.json()
      setStatus(data)
      if (!data.alive) {
        setSessionId(null)
        setConsoleEntries([])
        eventSourceRef.current?.close()
      } else {
        setSessionId(sid)
      }
    } catch {
      // ignore
    }
  }, [])

  // Setup SSE for transparency console
  const setupSSE = useCallback(() => {
    eventSourceRef.current?.close()
    const es = new EventSource(`${API}/events`, { withCredentials: true })
    es.onmessage = (evt) => {
      try {
        const entry: ConsoleEntry = JSON.parse(evt.data)
        setConsoleEntries((prev) => [...prev, entry])
      } catch { /* ignore */ }
    }
    es.onerror = () => {
      // SSE connection lost — will reconnect automatically
    }
    eventSourceRef.current = es
  }, [])

  // Check status on mount and after session operations
  useEffect(() => {
    fetchStatus()
  }, [fetchStatus])

  // Auto-scroll console
  useEffect(() => {
    if (consoleRef.current) {
      consoleRef.current.scrollTop = consoleRef.current.scrollHeight
    }
  }, [consoleEntries])

  // Handle session start
  const handleStart = async () => {
    if (!conversationName || !secretPhrase) return
    setLoading(true)
    try {
      const res = await fetch(`${API}/session/start`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ conversation_name: conversationName, secret_phrase: secretPhrase }),
      })
      const data = await res.json()
      if (data.error) {
        alert(data.error)
        return
      }
      setSessionId(data.session_id)
      if (data.audit_events) {
        setConsoleEntries(data.audit_events)
      }
      setSecretPhrase("") // wipe from memory
      setTab("encode")
      setupSSE()
      fetchStatus()
    } catch (err) {
      alert("Failed to start session")
    } finally {
      setLoading(false)
    }
  }

  // Handle encode
  const handleEncode = async () => {
    if (!plaintext) return
    setLoading(true)
    try {
      const res = await fetch(`${API}/message/encode`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ plaintext }),
      })
      const data = await res.json()
      if (data.error) {
        alert(data.error)
        return
      }
      setCoverText(data.cover_text)
      if (data.audit_events) {
        setConsoleEntries((prev) => [...prev, ...data.audit_events])
      }
      fetchStatus()
    } catch {
      alert("Encoding failed")
    } finally {
      setLoading(false)
    }
  }

  // Handle decode
  const handleDecode = async () => {
    if (!decodeInput) return
    setLoading(true)
    try {
      const res = await fetch(`${API}/message/decode`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ cover_text: decodeInput, sender: "remote" }),
      })
      const data = await res.json()
      if (data.error) {
        alert(data.error)
        return
      }
      setDecodedText(data.plaintext)
      if (data.audit_events) {
        setConsoleEntries((prev) => [...prev, ...data.audit_events])
      }
      fetchStatus()
    } catch {
      alert("Decoding failed")
    } finally {
      setLoading(false)
    }
  }

  // Handle revoke
  const handleRevoke = async () => {
    try {
      const res = await fetch(`${API}/session/revoke`, { method: "POST" })
      const data = await res.json()
      if (data.audit_events) {
        setConsoleEntries((prev) => [...prev, ...data.audit_events])
      }
    } catch { /* ignore */ }
    setSessionId(null)
    setStatus(null)
    setCoverText("")
    setDecodedText("")
    eventSourceRef.current?.close()
    setTab("setup")
    setTimeout(() => setConsoleEntries([]), 100)
  }

  const isSessionAlive = status?.alive === true
  const remaining = status?.remaining_seconds ?? 0
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
              <Badge variant="outline" className="text-xs font-mono">
                {remainingStr}
              </Badge>
              <Badge className="bg-[hsl(142,76%,36%)] hover:bg-[hsl(142,76%,36%)] text-white text-xs">
                Active
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
            <div className="space-y-2">
              <label className="text-sm font-medium">Conversation name</label>
              <input
                className="flex h-10 w-full rounded-lg border border-input bg-background px-3 py-2 text-sm ring-offset-background file:border-0 file:bg-transparent file:text-sm file:font-medium placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 disabled:cursor-not-allowed disabled:opacity-50"
                placeholder="e.g., secure-chat-01"
                value={conversationName}
                onChange={(e) => setConversationName(e.target.value)}
              />
            </div>
            <div className="space-y-2">
              <label className="text-sm font-medium">Secret phrase (min 16 chars)</label>
              <input
                type="password"
                className="flex h-10 w-full rounded-lg border border-input bg-background px-3 py-2 text-sm ring-offset-background file:border-0 file:bg-transparent file:text-sm file:font-medium placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 disabled:cursor-not-allowed disabled:opacity-50"
                placeholder="••••••••••••••••"
                value={secretPhrase}
                onChange={(e) => setSecretPhrase(e.target.value)}
              />
            </div>
            <Button
              className="w-full"
              onClick={handleStart}
              disabled={loading || conversationName.length === 0 || secretPhrase.length < 16}
            >
              {loading ? "Starting…" : "Start Secure Session"}
            </Button>
            {!isSessionAlive && sessionId && (
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
                  <Textarea
                    placeholder="Type your secret message here…"
                    value={plaintext}
                    onChange={(e) => setPlaintext(e.target.value)}
                  />
                </div>
                <Button
                  className="w-full"
                  onClick={handleEncode}
                  disabled={loading || !plaintext}
                >
                  {loading ? "Generating…" : "Generate Cover Text"}
                </Button>
                {coverText && (
                  <div className="space-y-2">
                    <label className="text-sm font-medium">Cover text</label>
                    <Textarea readOnly value={coverText} />
                    <Button
                      variant="secondary"
                      className="w-full"
                      onClick={() => navigator.clipboard.writeText(coverText)}
                    >
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
                  <Textarea
                    placeholder="Paste the received cover text here…"
                    value={decodeInput}
                    onChange={(e) => setDecodeInput(e.target.value)}
                  />
                </div>
                <Button
                  className="w-full"
                  onClick={handleDecode}
                  disabled={loading || !decodeInput}
                >
                  {loading ? "Decoding…" : "Decode Message"}
                </Button>
                {decodedText && (
                  <div className="space-y-2">
                    <label className="text-sm font-medium">Decrypted message</label>
                    <div className="p-4 rounded-lg bg-[hsl(142,30%,94%)] border border-[hsl(142,20%,85%)]">
                      <p className="text-sm">{decodedText}</p>
                    </div>
                  </div>
                )}
              </>
            )}
          </TabsContent>
        </Tabs>

        {/* Session footer */}
        {isSessionAlive && (
          <div className="px-6 pb-4 flex justify-between items-center">
            <Badge variant="outline" className="text-[10px] text-muted-foreground font-mono">
              session active
            </Badge>
            <button
              onClick={handleRevoke}
              className="text-xs text-destructive hover:underline"
            >
              Revoke session
            </button>
          </div>
        )}
      </Card>

      {/* Transparency Console */}
      <Card className="w-full max-w-xl overflow-hidden border-0 shadow-none">
        <div
          className="bg-black text-[#e0e0e0] rounded-xl border border-[#222] overflow-hidden"
        >
          <div className="flex items-center justify-between px-4 py-2 border-b border-[#222]">
            <span className="text-[10px] font-mono uppercase tracking-wider text-[#666]">Transparency Console</span>
            <span className="flex items-center gap-1.5">
              {isSessionAlive && (
                <span className="w-1.5 h-1.5 rounded-full bg-[hsl(142,70%,45%)] animate-pulse" />
              )}
              <span className="text-[10px] font-mono text-[#555]">
                {isSessionAlive ? "LIVE" : "INACTIVE"}
              </span>
            </span>
          </div>
          <ScrollArea className="h-48">
            <div ref={consoleRef} className="p-3 font-mono text-[11px] leading-relaxed space-y-1">
              {consoleEntries.length === 0 ? (
                <p className="text-[#444] italic">Waiting for events…</p>
              ) : (
                consoleEntries.map((entry, i) => (
                  <div key={i} className="animate-in fade-in duration-300">
                    <span className="text-[#555]">{entry.t ? entry.t.slice(11, 19) : ""}</span>
                    {" "}
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
