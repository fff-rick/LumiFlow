"use client"

import Link from "next/link"
import Image from "next/image"
import {
  Compass,
  LogIn,
  LogOut,
  Radio,
  Upload,
  UserRound,
  UsersRound,
} from "lucide-react"
import { usePathname } from "next/navigation"
import { useSession } from "@/components/SessionProvider"
import { assetURL } from "@/lib/platform"
import "./AppNav.css"

const links = [
  { href: "/discover", label: "发现", icon: Compass },
  { href: "/live", label: "直播", icon: Radio },
  { href: "/publish", label: "发布", icon: Upload },
  { href: "/me", label: "作品", icon: UserRound },
  { href: "/following", label: "关注", icon: UsersRound },
]

export default function AppNav() {
  const pathname = usePathname()
  const { user, loading, logout } = useSession()

  return (
    <header className="lumi-nav">
      <Link className="lumi-wordmark" href="/" aria-label="LumiFlow 首页">
        Lumi<span>Flow</span>
      </Link>
      <nav aria-label="主导航">
        {links.map(({ href, label, icon: Icon }) => (
          <Link
            className={isActive(pathname, href) ? "active" : ""}
            href={href}
            key={href}
          >
            <Icon size={16} />
            <span>{label}</span>
          </Link>
        ))}
      </nav>
      <div className="lumi-session">
        {loading ? (
          <span className="lumi-session-loading" />
        ) : user ? (
          <>
            <Link className="lumi-user" href="/me">
              {user.avatar_url ? (
                <Image
                  alt=""
                  height={24}
                  src={assetURL(user.avatar_url)!}
                  unoptimized
                  width={24}
                />
              ) : (
                <i>{(user.nickname || "L").slice(0, 1)}</i>
              )}
              <span>{user.nickname || "我的主页"}</span>
            </Link>
            <button
              aria-label="退出登录"
              onClick={() => void logout()}
              title="退出登录"
              type="button"
            >
              <LogOut size={16} />
            </button>
          </>
        ) : (
          <Link className="lumi-login" href="/login">
            <LogIn size={16} /> 登录
          </Link>
        )}
      </div>
    </header>
  )
}

function isActive(pathname: string, href: string) {
  if (href === "/live")
    return pathname === "/live" || pathname.startsWith("/rooms/")
  return pathname === href || pathname.startsWith(`${href}/`)
}
