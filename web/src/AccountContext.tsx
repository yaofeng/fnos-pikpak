import { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from 'react'
import { apiGetQuiet } from './api'
import { resolveCurrentId, type AccountInfo } from './accounts'

export type AccountContextValue = {
  accounts: AccountInfo[]
  byId: Map<string, AccountInfo>
  currentId: string
  current?: AccountInfo
  setCurrent(id: string): void
  reload(): Promise<void>
}

const AccountContext = createContext<AccountContextValue | null>(null)

// AccountProvider owns the account list the whole app shares. It reads
// GET /api/accounts on mount and again whenever something dispatches
// pikpak:account-changed (a bind, unbind or re-authorization), so every
// consumer — the quota pill, the reauth banners, the pages — sees the same
// list without each fetching its own copy.
export function AccountProvider({ children }: { children: ReactNode }) {
  const [accounts, setAccounts] = useState<AccountInfo[]>([])
  // The selection lives in memory only: a reload falls back to the first
  // account, which is also the backend's default for a bare uid.
  const [wantedId, setWantedId] = useState('')

  const reload = useCallback(async () => {
    try {
      const page = await apiGetQuiet<{ items?: AccountInfo[] }>('accounts')
      setAccounts(page.items ?? [])
    } catch {
      // Unbound users get a structured not_bound refusal here, and a network
      // hiccup fails the same quiet way: an empty list is the honest answer,
      // the pill and banners return with the next reload.
      setAccounts([])
    }
  }, [])

  useEffect(() => {
    void reload()
    const onChanged = () => void reload()
    window.addEventListener('pikpak:account-changed', onChanged)
    return () => window.removeEventListener('pikpak:account-changed', onChanged)
  }, [reload])

  const byId = useMemo(() => new Map(accounts.map((account) => [account.id, account])), [accounts])
  const currentId = resolveCurrentId(accounts, wantedId)
  const current = byId.get(currentId)

  const value = useMemo<AccountContextValue>(
    () => ({ accounts, byId, currentId, current, setCurrent: setWantedId, reload }),
    [accounts, byId, currentId, current, reload],
  )

  return <AccountContext.Provider value={value}>{children}</AccountContext.Provider>
}

// useAccounts throws outside the provider: a page rendering without it is a
// wiring bug, not an empty list to render.
export function useAccounts(): AccountContextValue {
  const ctx = useContext(AccountContext)
  if (!ctx) throw new Error('useAccounts must be used inside <AccountProvider>')
  return ctx
}
