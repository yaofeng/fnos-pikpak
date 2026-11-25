import { Navigate } from 'react-router-dom'

// The bind page folded into 设置's account section: adding or re-authorizing
// accounts now happens there directly, so any deep link lands on it.
export default function Bind() {
  return <Navigate to="/settings" replace />
}
