import { beforeEach, describe, expect, it, vi } from 'vitest'
import { ApiError } from './api'
import { downloadErrorMessage, queueDownloads } from './downloads'

const post = vi.hoisted(() => vi.fn())

vi.mock('./api', async () => {
  const actual = await vi.importActual<typeof import('./api')>('./api')
  return { ...actual, apiPost: post }
})

beforeEach(() => post.mockReset())

describe('queueDownloads', () => {
  it('reports how many files were queued', async () => {
    post.mockResolvedValue({ created: ['a', 'b'], skipped: [] })
    await expect(queueDownloads([{ file_id: 'a', account_id: 'a1' }, { file_id: 'b', account_id: 'a1' }])).resolves.toBe(
      '已加入下载队列：2 个。可在「传输」中查看进度。',
    )
    expect(post).toHaveBeenCalledWith('downloads', {
      items: [
        { file_id: 'a', account_id: 'a1' },
        { file_id: 'b', account_id: 'a1' },
      ],
    })
  })

  it('queueDownloads 发送 items 数组', async () => {
    // mock fetch，断言 body 为 {"items":[{file_id,account_id}]}
    post.mockResolvedValue({ created: ['a'], skipped: [] })
    await queueDownloads([{ file_id: 'f1', account_id: 'a1' }])
    expect(post).toHaveBeenCalledWith('downloads', { items: [{ file_id: 'f1', account_id: 'a1' }] })
  })

  it('says when a same-name file was skipped', async () => {
    post.mockResolvedValue({ created: ['a'], skipped: ['b'] })
    await expect(
      queueDownloads([
        { file_id: 'a', account_id: 'a1' },
        { file_id: 'b', account_id: 'a1' },
      ]),
    ).resolves.toBe('已加入下载队列：1 个，跳过 1 个同名文件。可在「传输」中查看进度。')
  })
})

describe('downloadErrorMessage', () => {
  it('points at the settings when no folder is configured', () => {
    const err = new ApiError(400, 'download_dir_unset', 'no dir', '')
    expect(downloadErrorMessage(err)).toBe('还没有设置默认下载目录，请到「设置 → 传输」指定一个目录。')
  })

  it('passes any other failure through', () => {
    expect(downloadErrorMessage(new Error('boom'))).toBe('boom')
  })
})
