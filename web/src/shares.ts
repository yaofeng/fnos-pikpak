import { isFolder, type FileItem } from './files'

// onlyFolderEntry returns the folder a share browser should enter without
// showing the root, when the root holds exactly one entry and that entry is a
// folder: there is nothing to choose, so the extra level is pure navigation.
// A root that still has another page is never skipped, or its other entries
// would have no way back.
export function onlyFolderEntry(items: FileItem[], nextPageToken: string): FileItem | null {
  if (nextPageToken) return null
  if (items.length !== 1) return null
  return isFolder(items[0]) ? items[0] : null
}
