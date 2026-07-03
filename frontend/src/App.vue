<script setup lang="ts">
import { ref, onMounted, onUnmounted, computed } from 'vue'
import { EventsOn } from '../wailsjs/runtime/runtime'
import {
  GetDevices, GetLocalDevice, SendFile, SendFilePath,
  SendMultipleFiles, SendMessage, GetMessages,
  StartP2P, StopP2P, GetP2PStatus, ConnectP2P,
  DisconnectP2P, SendP2PMessage, SendP2PFile, SendP2PFilePath,
  RefreshDevices, RespondTransfer,
  SendGroupMessage, ShareFile, ShareFilePath, ShareGroupFile, ShareGroupFilePath,
  DownloadSharedFile, GetGroups,
  GoOffline, GoOnline, IsOnline, GetSettings, ChooseDownloadDir, OpenFileInFolder,
  OpenFile, SetAskSaveLocation, SetDisplayName, RemoveKnownDevice
} from '../wailsjs/go/main/App'

import { models } from '../wailsjs/go/models'
import DeviceList from './components/DeviceList.vue'
import ChatPanel from './components/ChatPanel.vue'
import { useI18n } from './i18n'
import { tryDecrypt } from './utils/crypto'

const { t, te, localeLabel, toggleLocale } = useI18n()

// ---- Types ----
type Device = models.Device
type TransferRecord = models.TransferRecord
type Message = models.Message

// ---- State ----
const unreadCounts = ref<Record<string, number>>({})
const devices = ref<Device[]>([])
const groups = ref<any[]>([])
const localDevice = ref<Device | null>(null)
const selectedDevice = ref<Device | null>(null)
const selectedGroup = ref<any>(null)
const transferRequest = ref<any>(null) // pending file transfer request from another device
const allMessages = ref<Message[]>([])
const decryptedContent = ref<Record<string, string>>({}) // msgId → decrypted text
const downloadedShares = ref<Record<string, string>>({}) // shareId → saved path
const notification = ref<string | null>(null)

// P2P state
const p2pEnabled = ref(false)
const p2pLoading = ref(false)
const p2pPeerId = ref('')
const p2pConnString = ref('')
const p2pConnStringLocal = ref('')
const upnpStatus = ref<any>(null)
const natInfo = ref<any>(null)
const isOnline = ref(true)
const isToggling = ref(false)
const showSettings = ref(false)
const settingsDownloadDir = ref('')
const settingsAskSave = ref(false)
const settingsDeviceName = ref('')
const downloadResult = ref<{ name: string; path: string } | null>(null)
const showConnectModal = ref(false)
const p2pConnectInput = ref('')
const p2pConnecting = ref(false)
const isSending = ref(false)
const isSendingMessage = ref(false)

// ---- Computed ----
const chatTargetName = computed(() => {
  if (selectedGroup.value) return selectedGroup.value.name
  if (selectedDevice.value) return selectedDevice.value.name
  return ''
})

const totalUnread = computed(() =>
  Object.values(unreadCounts.value).reduce((a, b) => a + b, 0)
)

async function decryptStored() {
  for (const m of allMessages.value) {
    if (!decryptedContent.value[m.id]) await decryptMessage(m)
  }
}

async function decryptMessage(msg: Message) {
  for (const g of groups.value) {
    if (msg.deviceId === g.id && g.encrypted) {
      const dec = await tryDecrypt(msg.content, g.key || '', g.code)
      if (dec !== msg.content) decryptedContent.value[msg.id] = dec
      return
    }
  }
}

const chatMessages = computed(() => {
  if (!selectedDevice.value && !selectedGroup.value) return []
  const sid = selectedGroup.value?.id || selectedDevice.value?.id || ''
  return allMessages.value.filter(m => m.deviceId === sid)
    .map(m => ({ ...m, content: decryptedContent.value[m.id] || m.content }))
})

// ---- Methods ----
async function refreshDevices() {
  try {
    // Call Go to force re-emit devices-changed and get latest
    const list = await RefreshDevices()
    devices.value = list
    localDevice.value = await GetLocalDevice()
  } catch (e) {
    // Fallback: direct get
    try {
      devices.value = await GetDevices()
      localDevice.value = await GetLocalDevice()
    } catch (e2) { console.error('refreshDevices:', e2) }
  }
}

async function refreshHistory() {
  try {
    allMessages.value = await GetMessages()
  } catch (e) { console.error('refreshHistory:', e) }
}

async function refreshGroups() {
  try {
    groups.value = await GetGroups()
  } catch (e) { console.error('refreshGroups:', e) }
}

function selectDevice(device: Device) {
  if (selectedDevice.value?.id === device.id) {
    selectedDevice.value = null  // toggle off
    return
  }
  selectedDevice.value = device
  selectedGroup.value = null
  if (unreadCounts.value[device.id]) unreadCounts.value[device.id] = 0
}

function selectGroup(group: any) {
  if (selectedGroup.value?.id === group.id) {
    selectedGroup.value = null  // toggle off
    selectedDevice.value = null
    return
  }
  selectedGroup.value = group
  selectedDevice.value = null
}

function markAllRead() { unreadCounts.value = {} }

async function handleSendMessage(content: string) {
  isSendingMessage.value = true
  try {
    // Don't push here — the 'message-sent' event handles it (avoids duplicates)
    if (selectedGroup.value) {
      await SendGroupMessage(selectedGroup.value.id, content)
    } else if (selectedDevice.value) {
      if (selectedDevice.value.source === 'p2p') {
        await SendP2PMessage(selectedDevice.value.id, content)
      } else {
        await SendMessage(selectedDevice.value.ip, selectedDevice.value.id, selectedDevice.value.name, content)
      }
    }
  } catch (e) { showNotification(t('notification.sendFailed') + e) }
  finally { isSendingMessage.value = false }
}

async function handleSendFile(filePath: string) {
  isSending.value = true
  try {
    if (selectedGroup.value) {
      filePath
        ? await ShareGroupFilePath(selectedGroup.value.id, filePath)
        : await ShareGroupFile(selectedGroup.value.id)
    } else if (selectedDevice.value) {
      if (selectedDevice.value.source === 'p2p') {
        // P2P: still use push for now (no share server)
        filePath
          ? await SendP2PFilePath(selectedDevice.value.id, filePath)
          : await SendP2PFile(selectedDevice.value.id)
      } else {
        filePath
          ? await ShareFilePath(selectedDevice.value.ip, selectedDevice.value.id, selectedDevice.value.name, filePath)
          : await ShareFile(selectedDevice.value.ip, selectedDevice.value.id, selectedDevice.value.name)
      }
    }
  } catch (e) { showNotification(t('notification.fileFailed') + e) }
  finally { isSending.value = false }
}

async function handleSendMultipleFiles() {
  if (!selectedDevice.value || selectedDevice.value.source === 'p2p') return
  try {
    const records = await SendMultipleFiles(selectedDevice.value.ip, selectedDevice.value.id, selectedDevice.value.name)
    if (records) {
      showNotification(t('notification.filesSent', { n: records.length, name: selectedDevice.value.name }))
    }
  } catch (e) { showNotification(t('notification.fileFailed') + e) }
}

// ---- P2P ----
async function handleStartP2P() {
  p2pLoading.value = true
  try {
    const result = await StartP2P()
    upnpStatus.value = result
    await refreshP2PStatus()
  } catch (e) { showNotification(t('p2p.startFailed') + e) }
  finally { p2pLoading.value = false }
}

async function handleStopP2P() {
  p2pLoading.value = true
  try {
    await StopP2P()
    p2pEnabled.value = false; p2pPeerId.value = ''; p2pConnString.value = ''; upnpStatus.value = null
    await refreshDevices()
  } catch (e) { showNotification(t('p2p.stopFailed') + e) }
  finally { p2pLoading.value = false }
}

async function refreshP2PStatus() {
  try {
    const status = await GetP2PStatus()
    p2pEnabled.value = status.enabled
    p2pPeerId.value = status.peerId || ''
    p2pConnString.value = status.connectionString || ''
    p2pConnStringLocal.value = status.connectionStringLocal || ''
    upnpStatus.value = status.upnp || null
    natInfo.value = status.nat || null
  } catch (e) { console.error('refreshP2PStatus:', e) }
}

async function handleToggleOnline() {
  if (isToggling.value) return
  isToggling.value = true
  try {
    if (isOnline.value) {
      await GoOffline()
      isOnline.value = false
      p2pEnabled.value = false
    } else {
      await GoOnline()
      isOnline.value = true
      await refreshP2PStatus()
    }
    await refreshDevices()
  } catch (e) {
    showNotification('Toggle failed: ' + e)
  } finally {
    setTimeout(() => { isToggling.value = false }, 1500)
  }
}
async function handleRemoveDevice(id: string) {
  await RemoveKnownDevice(id)
  if (selectedDevice.value?.id === id) selectedDevice.value = null
  await refreshDevices()
}
async function handleConnectP2P() {
  const input = p2pConnectInput.value.trim()
  if (!input) return
  p2pConnecting.value = true
  try {
    await ConnectP2P(input, '')
    p2pConnectInput.value = ''
    showNotification(t('p2p.connected'))
    await refreshDevices()
  } catch (e) { showNotification(t('p2p.connectFailed') + e) }
  p2pConnecting.value = false
}

async function handleDisconnectP2P(peerID: string) {
  try {
    await DisconnectP2P(peerID)
    if (selectedDevice.value?.id === peerID) selectedDevice.value = null
    await refreshDevices()
  } catch (e) { showNotification(t('p2p.disconnectFailed') + e) }
}

function copyToClipboard(text: string) {
  navigator.clipboard.writeText(text)
  showNotification(t('p2p.copied'))
}

function showNotification(msg: string) {
  notification.value = msg
  setTimeout(() => { notification.value = null }, 3000)
}

async function openSettings() {
  try {
    const s = await GetSettings()
    settingsDownloadDir.value = s.downloadDir || ''
    settingsAskSave.value = s.askSaveLocation || false
    settingsDeviceName.value = localDevice.value?.name || ''
    showSettings.value = true
  } catch (e) { console.error(e) }
}

async function saveDeviceName() {
  const name = settingsDeviceName.value.trim()
  if (!name) return
  await SetDisplayName(name)
  await refreshDevices()
  showNotification(t('settings.deviceNameSaved'))
}

async function toggleAskSave() {
  settingsAskSave.value = !settingsAskSave.value
  await SetAskSaveLocation(settingsAskSave.value)
}

async function chooseDownloadDir() {
  try {
    const dir = await ChooseDownloadDir()
    if (dir) {
      settingsDownloadDir.value = dir
      showNotification(t('settings.changed'))
    }
  } catch (e) { showNotification('Error: ' + e) }
}

async function acceptTransfer() {
  if (!transferRequest.value) return
  await RespondTransfer(transferRequest.value.id, true)
  transferRequest.value = null
}

async function rejectTransfer() {
  if (!transferRequest.value) return
  await RespondTransfer(transferRequest.value.id, false)
  transferRequest.value = null
}

function formatFileSize(bytes: number): string {
  if (!bytes || bytes === 0) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB']
  const i = Math.floor(Math.log(bytes) / Math.log(1024))
  return (bytes / Math.pow(1024, i)).toFixed(i > 0 ? 1 : 0) + ' ' + units[i]
}

function translateError(result: string): string {
  if (result.includes('dial tcp') || result.includes('connectex') || result.includes('i/o timeout')) {
    return t('errors.connectFailed')
  }
  if (result.includes('no such host')) {
    return t('errors.dnsFailed')
  }
  if (result.includes('connection refused')) {
    return t('errors.refused')
  }
  if (result.startsWith('save failed')) {
    return t('errors.saveFailed') + result.replace(/^save failed:\s*/, '')
  }
  return result
}

async function handleDownloadFile(shareInfo: any) {
  // Prefer the currently-selected device's IP (handles re-connect with new IP).
  // Fall back to the IP embedded in the share message.
  let ip = shareInfo.senderIP
  if (selectedDevice.value?.id && devices.value.length) {
    const dev = devices.value.find(d => d.id === selectedDevice.value!.id)
    if (dev?.ip) ip = dev.ip
  }
  try {
    const result = await DownloadSharedFile(ip, shareInfo.shareId, shareInfo.fileName)
    if (result && !result.startsWith('download failed') && !result.startsWith('save failed') && result !== 'cancelled') {
      downloadResult.value = { name: shareInfo.fileName, path: result }
      downloadedShares.value[shareInfo.shareId] = result
    } else if (result === 'cancelled') {
      // silent
    } else {
      showNotification('❌ ' + translateError(result))
    }
  } catch (e) {
    showNotification('❌ ' + t('errors.downloadFailed') + ': ' + e)
  }
}

async function openDownloadedFolder(path?: string) {
  const p = path || downloadResult.value?.path
  if (!p) {
    console.warn('openDownloadedFolder: no path')
    return
  }
  console.log('openDownloadedFolder:', p)
  const result = await OpenFileInFolder(p)
  if (result && result !== 'ok') {
    showNotification('❌ ' + result)
  }
}

async function openDownloadedFile() {
  if (downloadResult.value) {
    await OpenFile(downloadResult.value.path)
    downloadResult.value = null
  }
}

function copyDownloadPath() {
  if (downloadResult.value) {
    navigator.clipboard.writeText(downloadResult.value.path).catch(() => {})
    showNotification(t('settings.copyPath') + ' ✓')
  }
}

// ---- Lifecycle ----
onMounted(async () => {
  await refreshDevices()
	await refreshGroups()
  await refreshHistory()
  await refreshP2PStatus()

  EventsOn('devices-changed', async (newDevices: Device[]) => {
    if (localDevice.value) {
      devices.value = [localDevice.value, ...newDevices.filter(d => d.id !== localDevice.value!.id)]
    } else { devices.value = newDevices }
  })

  EventsOn('file-received', (record: TransferRecord) => {
    showNotification(t('notification.fileReceived', { file: record.fileName, name: record.deviceName }))
  })

  EventsOn('message-sent', (msg: Message) => {
    if (!allMessages.value.find(m => m.id === msg.id)) {
      allMessages.value.push(msg)
    }
  })

  EventsOn('message-received', async (msg: Message) => {
    allMessages.value.push(msg)
    // Try decrypt if this message is for a group
    await decryptMessage(msg)
    if (selectedDevice.value?.id !== msg.deviceId) {
      let preview = decryptedContent.value[msg.id] || msg.content
      if (preview.length > 50) preview = preview.slice(0, 50)
      showNotification(t('notification.messageFrom', { name: msg.deviceName, msg: preview }))
      const cur = unreadCounts.value[msg.deviceId] || 0
      unreadCounts.value[msg.deviceId] = cur + 1
    }
  })

  EventsOn('transfer-update', (_record: TransferRecord) => {
    // transfer status updates handled via download modal
  })

  EventsOn('p2p-started', () => { p2pEnabled.value = true; refreshP2PStatus() })
	EventsOn("groups-changed", () => { refreshGroups() })

  EventsOn('transfer-request', (req: any) => {
    transferRequest.value = req
  })
})

onUnmounted(() => {})
</script>

<template>
  <div class="app-layout">
    <div v-if="notification" class="toast">{{ notification }}</div>

    <!-- Sidebar -->
    <div class="sidebar">
      <div class="logo">
        <span class="logo-icon">🔗</span>
        <span class="logo-text">{{ t('app.title') }}</span>
        <button class="btn-lang" @click="openSettings">⚙</button>
        <button class="btn-lang" @click="toggleLocale">{{ localeLabel }}</button>
      </div>

      <div class="sidebar-body">
                <div class="toolbar" v-if="totalUnread > 0">
          <button class="btn-read-all" @click="markAllRead">
            ✅ {{ t('device.markAllRead') }} ({{ totalUnread }})
          </button>
        </div>
        <DeviceList
          :devices="devices"
          :localDeviceId="localDevice?.id ?? ''"
          :selectedDeviceId="selectedDevice?.id ?? null"
          :selectedGroupId="selectedGroup?.id ?? null"
          :unreadCounts="unreadCounts"
          :groups="groups"
          :isOnline="isOnline"
          :isToggling="isToggling"
          @select="selectDevice"
          @selectGroup="selectGroup"
          @groupsChanged="refreshGroups"
          @toggleOnline="handleToggleOnline"
          @refresh="refreshDevices"
          @openConnect="showConnectModal = true"
          @removeDevice="handleRemoveDevice"
        />
      </div>
    </div>

    <!-- Main Content -->
    <div class="main-content">
      <div v-if="!selectedDevice && !selectedGroup" class="empty-state">
        <div class="empty-icon">👋</div>
        <h2>{{ t('app.welcome') }}</h2>
        <p>{{ t('app.welcomeDesc') }}</p>
        <p class="empty-hint">{{ t('app.welcomeHint') }}</p>
      </div>

      <div v-else class="device-workspace">
        <div class="workspace-header">
          <div class="header-device-info">
            <span class="header-icon">{{ selectedGroup ? '👥' : selectedDevice?.source === 'p2p' ? '🌐' : '💻' }}</span>
            <div>
              <div class="header-name">
                {{ selectedGroup ? selectedGroup.name : selectedDevice?.name }}
                <span v-if="selectedGroup" class="source-tag group-tag">👥 {{ t('group.broadcast') }}</span>
                <span v-else-if="selectedDevice" class="source-tag">{{ selectedDevice.source === 'p2p' ? t('source.p2p') : t('source.lan') }}</span>
              </div>
              <div class="header-ip" v-if="!selectedGroup">{{ selectedDevice?.ip || selectedDevice?.id?.slice(0, 16) + '...' }}</div>
              <div class="header-ip" v-else>{{ t('group.broadcast') }} · {{ selectedGroup.members.length }} {{ t('group.members') }}</div>
            </div>
          </div>
          <div class="header-tabs">
            <button v-if="!selectedGroup && selectedDevice?.source === 'p2p'" class="btn-disconnect" @click="handleDisconnectP2P(selectedDevice!.id)">✕</button>
          </div>
        </div>

        <div class="workspace-body">
          <ChatPanel
            :messages="chatMessages"
            :deviceName="chatTargetName"
            :localDeviceId="localDevice?.id ?? ''"
            :sending="isSending"
            :sendingMessage="isSendingMessage"
            :downloadedShares="downloadedShares"
            @send="handleSendMessage"
            @sendFile="handleSendFile"
            @download="handleDownloadFile"
            @openFolder="openDownloadedFolder"
          />
        </div>
      </div>
    </div>

    <!-- Transfer Request Modal -->
    <div v-if="transferRequest" class="modal-overlay">
      <div class="modal transfer-modal">
        <div class="modal-title">📥 {{ t('transfer.requestTitle') }}</div>
        <p>{{ t('transfer.requestFrom', { name: transferRequest.senderName }) }}</p>
        <p class="request-file">{{ t('transfer.requestInfo', { file: transferRequest.fileName, size: formatFileSize(transferRequest.fileSize) }) }}</p>
        <div class="modal-btns">
          <button class="btn-cancel" @click="rejectTransfer">{{ t('transfer.reject') }}</button>
          <button class="btn-confirm" @click="acceptTransfer">{{ t('transfer.accept') }}</button>
        </div>
      </div>
    </div>

    <!-- Download Complete Modal -->
    <div v-if="downloadResult" class="modal-overlay" @click.self="downloadResult = null">
      <div class="modal">
        <div class="modal-title">✅ {{ t('settings.downloadComplete') }}</div>
        <div class="setting-label">{{ downloadResult.name }}</div>
        <div class="setting-hint">{{ t('settings.downloadPath') }}:</div>
        <div class="download-path-box"><code>{{ downloadResult.path }}</code></div>
        <div class="modal-btns">
          <button class="btn-cancel" @click="copyDownloadPath">{{ t('settings.copyPath') }}</button>
          <button class="btn-cancel" @click="openDownloadedFolder()">{{ t('settings.openFolder') }}</button>
          <button class="btn-confirm" @click="openDownloadedFile">{{ t('settings.openFile') }}</button>
        </div>
      </div>
    </div>

    <!-- P2P Connect Modal -->
    <div v-if="showConnectModal" class="modal-overlay" @click.self="showConnectModal = false">
      <div class="modal connect-modal">
        <div class="modal-title">
          🌐 {{ t('p2p.title') }}
          <span v-if="p2pEnabled" class="p2p-status-on">🟢 {{ t('p2p.running') }}</span>
          <span v-else class="p2p-status-off">⚫ {{ t('p2p.upnpChecking') }}</span>
        </div>

        <div v-if="p2pEnabled" class="connect-body">
          <!-- My PeerID -->
          <div class="connect-section">
            <div class="setting-label">{{ t('p2p.peerId') }}</div>
            <div class="connect-row">
              <code class="connect-code">{{ p2pPeerId.slice(0, 20) }}…</code>
              <button class="btn-mini" @click="copyToClipboard(p2pPeerId)">📋</button>
            </div>
          </div>

          <!-- Connection string -->
          <div class="connect-section">
            <div class="setting-label">{{ upnpStatus?.enabled ? t('p2p.shareHint') : t('p2p.shareHintLocal') }}</div>
            <textarea readonly rows="2" :value="upnpStatus?.enabled ? p2pConnString : p2pConnStringLocal" class="conn-str"></textarea>
            <button class="btn-mini btn-copy-conn" @click="copyToClipboard(upnpStatus?.enabled ? p2pConnString : p2pConnStringLocal)">📋 {{ t('p2p.copy') }}</button>
          </div>

          <!-- Manual connect (advanced) -->
          <div class="connect-section">
            <div class="setting-label">🔗 {{ t('p2p.connect') }}</div>
            <div class="setting-hint">{{ t('p2p.pastePlaceholder') }}</div>
            <div class="connect-row">
              <input v-model="p2pConnectInput" :placeholder="t('p2p.pastePlaceholder')" class="setting-input" />
              <button class="btn-confirm" :disabled="p2pConnecting || !p2pConnectInput" @click="handleConnectP2P">{{ p2pConnecting ? '…' : t('p2p.connect') }}</button>
            </div>
          </div>
        </div>

        <div v-else class="connect-body">
          <div class="setting-hint">{{ t('p2p.upnpChecking') }}</div>
        </div>

        <div class="modal-btns">
          <button class="btn-confirm full" @click="showConnectModal = false">OK</button>
        </div>
      </div>
    </div>

    <!-- Settings Modal -->
    <div v-if="showSettings" class="modal-overlay" @click.self="showSettings = false">
      <div class="modal">
        <div class="modal-title">⚙ {{ t('settings.title') }}</div>
        <div class="setting-row">
          <div class="setting-label">🏷 {{ t('settings.deviceName') }}</div>
          <div class="setting-input-row">
            <input v-model="settingsDeviceName" class="setting-input" :placeholder="t('settings.deviceName')" @keydown.enter="saveDeviceName" />
            <button class="btn-confirm" @click="saveDeviceName">{{ t('settings.save') }}</button>
          </div>
        </div>
        <div class="setting-row">
          <div class="setting-label">📁 {{ t('settings.downloadDir') }}</div>
          <div class="setting-hint">{{ t('settings.downloadDirHint') }}</div>
          <div class="setting-input-row">
            <input :value="settingsDownloadDir" readonly class="setting-input" />
            <button class="btn-confirm" @click="chooseDownloadDir">{{ t('settings.choose') }}</button>
          </div>
        </div>
        <div class="setting-row toggle-row" @click="toggleAskSave">
          <span class="setting-label">{{ t('settings.askSaveLocation') }}</span>
          <span class="toggle-switch" :class="{ on: settingsAskSave }">
            <span class="toggle-knob"></span>
          </span>
        </div>
        <div class="modal-btns">
          <button class="btn-confirm" @click="showSettings = false">OK</button>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.app-layout { display: flex; height: 100vh; background: var(--bg-primary); }

.sidebar {
  width: 300px; min-width: 300px; background: var(--bg-secondary);
  border-right: 1px solid var(--border); display: flex; flex-direction: column; overflow: hidden;
}

.logo {
  display: flex; align-items: center; gap: 8px;
  padding: 12px 16px; font-size: 18px; font-weight: 700;
  border-bottom: 1px solid var(--border); flex-shrink: 0;
}
.logo-icon { font-size: 22px; }
.logo-text { flex: 1; }

.btn-lang {
  padding: 2px 8px; font-size: 11px; font-weight: 600;
  background: var(--bg-card); color: var(--text-secondary);
  border: 1px solid var(--border); border-radius: 4px;
  cursor: pointer; transition: all 0.15s;
}
.btn-lang:hover { background: var(--accent); color: #fff; border-color: var(--accent); }


/* Sidebar Body */
.sidebar-body { flex: 1; overflow-y: auto; }

.toolbar { padding: 4px 8px 0; }
.toolbar .btn-read-all {
  width: 100%; padding: 4px; font-size: 10px; white-space: nowrap;
  background: var(--danger); color: #fff;
  border: none; border-radius: 4px; cursor: pointer; font-weight: 600;
}
.toolbar .btn-read-all:hover { opacity: 0.85; }
.btn-read-all {
  padding: 4px 8px; font-size: 10px; white-space: nowrap;
  background: var(--danger); color: #fff;
  border: none; border-radius: 4px; cursor: pointer; font-weight: 600;
}
.btn-read-all:hover { opacity: 0.85; }

/* P2P */
.p2p-section { padding: 8px; }
.p2p-toggle-row { padding: 0 0 8px; display: flex; justify-content: center; }
.p2p-status-on { font-size: 10px; color: var(--success); font-weight: 600; }
.p2p-status-off { font-size: 10px; color: var(--text-secondary); }
.pair-hint { font-size: 10px; color: var(--text-secondary); margin-bottom: 4px; line-height: 1.4; }
.btn-p2p-toggle { padding: 6px 20px; font-size: 12px; border-radius: 4px; transition: all 0.15s; width: 100%; }
.btn-p2p-toggle.start { background: var(--success); color: #fff; }
.btn-p2p-toggle.stop { background: var(--danger); color: #fff; }
.btn-p2p-toggle:disabled { opacity: 0.5; cursor: wait; }
.p2p-info { font-size: 11px; }
.p2p-subsection { padding: 6px 0; border-top: 1px solid var(--border); margin-top: 6px; }
.p2p-subsection:first-of-type { border-top: none; margin-top: 2px; }
.p2p-subtitle { font-size: 10px; font-weight: 600; color: var(--text-secondary); text-transform: uppercase; margin-bottom: 4px; }
.p2p-row { display: flex; align-items: center; gap: 6px; padding: 3px 4px; }
.p2p-row.ok { color: var(--success); }
.p2p-row.warn { color: var(--warning); }
.p2p-ok { color: var(--success); font-weight: 600; }
.p2p-label { color: var(--text-secondary); min-width: 52px; font-weight: 500; flex-shrink: 0; }
.p2p-code { font-size: 10px; color: var(--accent); background: var(--bg-primary); padding: 1px 4px; border-radius: 3px; }
.p2p-protocol-tag { font-size: 8px; padding: 1px 4px; border-radius: 3px; background: var(--success); color: #fff; font-weight: 600; }
.p2p-error { font-size: 10px; color: var(--danger); padding: 4px; background: rgba(248, 81, 73, 0.08); border-radius: 4px; margin: 4px 0; line-height: 1.4; word-break: break-all; }
.p2p-help { font-size: 10px; color: var(--text-secondary); padding: 4px; line-height: 1.5; white-space: pre-line; }
.conn-str { width: 100%; margin-top: 2px; font-size: 9px; font-family: monospace; resize: none; padding: 4px; background: var(--bg-primary); border: 1px solid var(--border); border-radius: 4px; color: var(--text-primary); }
.btn-copy-conn { margin-top: 2px; }
.p2p-connect { display: flex; gap: 4px; padding: 4px 0; }
.connect-input { flex: 1; font-size: 10px; padding: 4px 6px; height: 24px; }
.connect-btn { padding: 2px 8px; font-size: 10px; height: 24px; }
.btn-mini { padding: 1px 6px; font-size: 10px; background: var(--bg-card); border: 1px solid var(--border); border-radius: 3px; color: var(--text-secondary); cursor: pointer; }
.btn-mini:hover { background: var(--bg-hover); color: var(--text-primary); }

.main-content { flex: 1; display: flex; flex-direction: column; overflow: hidden; }

.empty-state { flex: 1; display: flex; flex-direction: column; align-items: center; justify-content: center; gap: 8px; color: var(--text-secondary); }
.empty-icon { font-size: 56px; margin-bottom: 8px; }
.empty-state h2 { font-size: 22px; font-weight: 600; color: var(--text-primary); }
.empty-state p { font-size: 13px; white-space: pre-line; text-align: center; }
.empty-hint { font-size: 12px !important; margin-top: 16px; line-height: 1.6; }

.device-workspace { flex: 1; display: flex; flex-direction: column; overflow: hidden; }
.workspace-header { display: flex; align-items: center; justify-content: space-between; padding: 10px 16px; border-bottom: 1px solid var(--border); background: var(--bg-secondary); flex-shrink: 0; }
.header-device-info { display: flex; align-items: center; gap: 10px; }
.header-icon { font-size: 22px; }
.header-name { font-size: 14px; font-weight: 600; }
.header-ip { font-size: 11px; color: var(--text-secondary); font-family: monospace; }
.source-tag { font-size: 9px; padding: 1px 5px; border-radius: 3px; background: var(--bg-card); color: var(--accent); text-transform: uppercase; vertical-align: middle; margin-left: 6px; }
.header-tabs { display: flex; gap: 4px; }
.header-tabs button { background: transparent; color: var(--text-secondary); padding: 6px 14px; font-size: 12px; }
.header-tabs button.active { background: var(--accent); color: #fff; }
.header-tabs button:hover:not(.active):not(.btn-disconnect) { background: var(--bg-hover); color: var(--text-primary); }
.btn-disconnect { color: var(--danger) !important; font-weight: bold; }
.btn-disconnect:hover { background: var(--danger) !important; color: #fff !important; }
.workspace-body { flex: 1; overflow: hidden; }

.toast { position: fixed; top: 16px; left: 50%; transform: translateX(-50%); background: var(--bg-card); border: 1px solid var(--border); color: var(--text-primary); padding: 10px 20px; border-radius: var(--radius); font-size: 13px; box-shadow: var(--shadow); z-index: 1000; animation: toast-in 0.25s ease; }
.group-tag { background: rgba(88,166,255,0.2) !important; color: var(--accent) !important; }

.modal-overlay { position: fixed; inset: 0; background: rgba(0,0,0,0.5); display: flex; align-items: center; justify-content: center; z-index: 100; }
.modal { background: var(--bg-secondary); border: 1px solid var(--border); border-radius: var(--radius); padding: 24px; min-width: 300px; box-shadow: var(--shadow); }
.modal-title { font-size: 16px; font-weight: 600; margin-bottom: 8px; }
.modal p { font-size: 13px; color: var(--text-secondary); margin-bottom: 4px; }
.request-file { font-size: 13px; color: var(--text-primary) !important; background: var(--bg-primary); padding: 8px 12px; border-radius: 6px; margin: 12px 0 !important; }
.modal-btns { display: flex; gap: 8px; justify-content: flex-end; margin-top: 16px; }
.btn-cancel { padding: 6px 18px; background: transparent; color: var(--text-secondary); border: 1px solid var(--border); border-radius: 4px; cursor: pointer; font-size: 13px; }
.btn-confirm { padding: 6px 18px; background: var(--accent); color: #fff; border: none; border-radius: 4px; cursor: pointer; font-weight: 600; font-size: 13px; }

.setting-row { margin-bottom: 12px; }
.setting-label { font-size: 13px; font-weight: 600; margin-bottom: 4px; }
.setting-hint { font-size: 11px; color: var(--text-secondary); margin-bottom: 8px; }
.setting-input-row { display: flex; gap: 8px; }
.setting-input { flex: 1; font-size: 12px; font-family: monospace; padding: 6px 10px; background: var(--bg-primary); border: 1px solid var(--border); border-radius: 4px; color: var(--text-primary); }

.download-path-box {
  background: var(--bg-primary); border: 1px solid var(--border); border-radius: 6px;
  padding: 8px 12px; margin: 6px 0 4px; word-break: break-all;
}
.download-path-box code { font-size: 12px; color: var(--accent); }
.btn-confirm.full { width: 100%; margin-top: 4px; }

.connect-modal { width: 380px; max-height: 80vh; overflow-y: auto; }
.connect-body { margin: 8px 0; }
.connect-section { margin-bottom: 14px; padding-bottom: 12px; border-bottom: 1px solid var(--border); }
.connect-section:last-child { border-bottom: none; }
.connect-row { display: flex; gap: 6px; align-items: center; margin-top: 4px; }
.connect-code { flex: 1; font-size: 11px; font-family: monospace; background: var(--bg-primary); padding: 6px 8px; border-radius: 4px; color: var(--accent); word-break: break-all; }
.connect-modal .conn-str { width: 100%; margin: 4px 0; box-sizing: border-box; }
.connect-modal .setting-input { flex: 1; }
.connect-modal .name-input { max-width: 100px; flex: 0 0 100px; }

.toggle-row { display: flex; align-items: center; justify-content: space-between; padding: 10px 0; cursor: pointer; border-top: 1px solid var(--border); }
.toggle-row:hover { background: var(--bg-hover); margin: 0 -24px; padding: 10px 24px; }
.toggle-switch { width: 36px; height: 20px; background: var(--bg-primary); border-radius: 10px; position: relative; transition: background 0.2s; border: 1px solid var(--border); flex-shrink: 0; }
.toggle-switch.on { background: var(--success); border-color: var(--success); }
.toggle-knob { position: absolute; top: 1px; left: 1px; width: 16px; height: 16px; background: #fff; border-radius: 50%; transition: left 0.2s; }
.toggle-switch.on .toggle-knob { left: 17px; }
@keyframes toast-in { from { opacity: 0; transform: translateX(-50%) translateY(-10px); } to { opacity: 1; transform: translateX(-50%) translateY(0); } }
</style>
