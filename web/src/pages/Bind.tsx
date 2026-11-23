import { BindCard } from '../components/BindCard'

export default function Bind() {
  return (
    <>
      <p className="muted">
        每个飞牛用户绑定自己的 PikPak 账号；绑定信息只保存在本机，其他飞牛用户看不到你的文件。
      </p>
      <BindCard onChange={() => window.dispatchEvent(new Event('pikpak:account-changed'))} />
    </>
  )
}
