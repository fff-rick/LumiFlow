import { NextResponse } from "next/server"

const sessionCookie = "lumiflow_access_token"

export async function DELETE() {
  const response = new NextResponse(null, { status: 204 })
  response.cookies.set(sessionCookie, "", {
    httpOnly: true,
    sameSite: "lax",
    secure: process.env.NODE_ENV === "production",
    path: "/",
    maxAge: 0,
  })
  return response
}
