import TrimApp from '@trimjs/web-app'

import { pathsFrom } from './sdkPaths'
import { getLang, makeT } from './i18n'

let instance: TrimApp | null | undefined

// sdk returns the fnOS host bridge, or null when the page is running in a
// plain browser (make dev) where no host answers.
function sdk(): TrimApp | null {
  if (instance !== undefined) return instance
  try {
    instance = new TrimApp()
  } catch {
    instance = null
  }
  return instance
}

// hostAvailable reports whether the page runs inside the fnOS app shell, where
// the system pickers and the file manager exist.
export function hostAvailable(): boolean {
  const app = sdk()
  return !!app && !app.isStandaloneWeb
}

export async function pickDirectory(title: string): Promise<string[]> {
  const app = sdk()
  if (!app) return []
  try {
    return pathsFrom(await app.pickUserFile({ directory: true, multiple: false, title }))
  } catch {
    return []
  }
}

export async function pickFiles(title: string): Promise<string[]> {
  const app = sdk()
  if (!app) return []
  try {
    return pathsFrom(await app.pickUserFile({ directory: false, multiple: true, title }))
  } catch {
    return []
  }
}

// openFolder reveals a folder in the fnOS file manager. It returns false when
// the host is unavailable so the caller can hide the action instead of
// offering a button that does nothing.
export async function openFolder(path: string): Promise<boolean> {
  const app = sdk()
  if (!app || app.isStandaloneWeb || !path) return false
  try {
    await app.openFileManager(path)
    return true
  } catch {
    return false
  }
}

// askPath is the development fallback for the system picker: it asks for an
// absolute path so transfer flows can be exercised with `make dev`.
export function askPath(label: string): string | null {
  const value = window.prompt(label)?.trim()
  if (!value) return null
  if (!value.startsWith('/')) {
    window.alert(makeT(getLang())('settings.ask_path_invalid'))
    return null
  }
  return value
}
