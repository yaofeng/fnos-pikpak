// Settings are split in two scopes: the transfer queue reads its concurrency
// from the global table, while conflict policy, play mode and language are
// per fnOS user. Keeping the key lists here stops a form from writing one
// scope into the other.
export type SettingValues = Record<string, string>

export const globalSettingKeys = ['download_concurrency', 'upload_concurrency', 'search_api_key'] as const
export const userSettingKeys = ['conflict', 'play_mode', 'language', 'download_dir'] as const

export function pickSettings(all: SettingValues, keys: readonly string[]): SettingValues {
  const out: SettingValues = {}
  for (const key of keys) {
    if (all[key] !== undefined) out[key] = all[key]
  }
  return out
}

// changedSettings returns only the keys whose value differs, so a save does not
// rewrite settings the user never touched.
export function changedSettings(current: SettingValues, next: SettingValues): SettingValues {
  const out: SettingValues = {}
  for (const [key, value] of Object.entries(next)) {
    if (current[key] !== value) out[key] = value
  }
  return out
}

// unbindWarning is shown before the account is dropped. It must name both
// losses: the stored PikPak credentials and the local transfer records.
export const unbindWarning =
  '解绑会删除本机保存的 PikPak 凭据，并清除本用户的全部传输任务记录（已下载的文件保留）。解绑后需要重新授权才能继续使用。'
