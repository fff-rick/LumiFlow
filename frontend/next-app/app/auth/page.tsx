"use client"

import { useEffect } from "react"
import { useRouter } from "next/navigation"

// FluxFeed completes OAuth at /auth#oauth-code=... . Keep that public
// contract while handing the code to the fused login page.
export default function AuthCallbackPage() {
  const router = useRouter()

  useEffect(() => {
    router.replace(`/login${window.location.hash}`)
  }, [router])

  return null
}
