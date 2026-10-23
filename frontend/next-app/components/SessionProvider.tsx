"use client"

import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
} from "react"
import { useRouter } from "next/navigation"
import { PlatformError, platformRequest, SessionUser } from "@/lib/platform"

type SessionState = {
  user: SessionUser | null
  loading: boolean
  refreshSession: () => Promise<SessionUser | null>
  logout: () => Promise<void>
}

const SessionContext = createContext<SessionState | null>(null)

export default function SessionProvider({
  children,
}: {
  children: React.ReactNode
}) {
  const router = useRouter()
  const [user, setUser] = useState<SessionUser | null>(null)
  const [loading, setLoading] = useState(true)

  const refreshSession = useCallback(async () => {
    try {
      const value = await platformRequest<SessionUser>("/me")
      setUser(value)
      return value
    } catch (error) {
      if (
        error instanceof PlatformError &&
        (error.status === 401 || error.status === 403)
      ) {
        await fetch("/api/session", { method: "DELETE" })
      }
      setUser(null)
      return null
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    void Promise.resolve().then(refreshSession)
  }, [refreshSession])

  const logout = useCallback(async () => {
    await fetch("/api/session", { method: "DELETE" })
    setUser(null)
    router.push("/")
    router.refresh()
  }, [router])

  const value = useMemo(
    () => ({ user, loading, refreshSession, logout }),
    [loading, logout, refreshSession, user]
  )

  return (
    <SessionContext.Provider value={value}>{children}</SessionContext.Provider>
  )
}

export function useSession() {
  const value = useContext(SessionContext)
  if (!value) throw new Error("useSession must be used within SessionProvider")
  return value
}
