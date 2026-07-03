<script setup lang="ts">
import { ref, watch, nextTick, onMounted } from 'vue'
import { models } from '../../wailsjs/go/models'
import { useI18n } from '../i18n'

type Message = models.Message

const { t } = useI18n()

const props = defineProps<{
  messages: Message[]
  deviceName: string
  localDeviceId: string
  sending?: boolean
  sendingMessage?: boolean
  downloadedShares?: Record<string, string> // shareId → saved path
}>()

const emit = defineEmits<{
  send: [content: string]
  sendFile: [filePath: string]
  download: [shareInfo: any]
  openFolder: [path: string]
}>()

function tryParseShare(content: string): any | null {
  try {
    const obj = JSON.parse(content)
    if (obj.type === 'share') return obj
  } catch {}
  return null
}

function isDownloaded(share: any): boolean {
  return !!(props.downloadedShares && props.downloadedShares[share.shareId])
}

function getPath(share: any): string {
  return props.downloadedShares?.[share.shareId] || ''
}

// For sent files, the sender's local path lets them open the source folder.
// For received files, use the downloaded path.
function canOpenFolder(share: any, direction: string): boolean {
  if (direction === 'sent') return !!share.senderPath
  return isDownloaded(share)
}

function getOpenPath(share: any, direction: string): string {
  if (direction === 'sent') return share.senderPath || ''
  return getPath(share)
}

function formatSize(bytes: number): string {
  if (!bytes || bytes === 0) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB']
  const i = Math.floor(Math.log(bytes) / Math.log(1024))
  return (bytes / Math.pow(1024, i)).toFixed(i > 0 ? 1 : 0) + ' ' + units[i]
}

const inputText = ref('')
const messagesEl = ref<HTMLElement | null>(null)
const isDragOver = ref(false)

watch(() => props.messages.length, () => {
  nextTick(() => {
    if (messagesEl.value) messagesEl.value.scrollTop = messagesEl.value.scrollHeight
  })
})

onMounted(() => {
  nextTick(() => messagesEl.value?.scrollTo(0, messagesEl.value.scrollHeight))

  // Wails native drag & drop — must use OnFileDrop() to register, not EventsOn
  // Callback signature: (x, y, paths[])
  const wailsRuntime = (window as any).runtime
  if (wailsRuntime?.OnFileDrop) {
    wailsRuntime.OnFileDrop((_x: number, _y: number, paths: string[]) => {
      if (paths?.length) {
        for (const p of paths) if (p) emit('sendFile', p)
      }
    })
  }
})

function handleSend() {
  if (props.sendingMessage) return
  const text = inputText.value.trim()
  if (!text) return
  emit('send', text)
  inputText.value = ''
}

function handleKeydown(e: KeyboardEvent) {
  if (e.key === 'Enter' && !e.shiftKey) { e.preventDefault(); handleSend() }
}

function handleAttachClick() { emit('sendFile', '') }

function handleDragOver(e: DragEvent) {
  e.preventDefault()
  if (e.dataTransfer) { e.dataTransfer.dropEffect = 'copy'; isDragOver.value = true }
}
function handleDragLeave(e: DragEvent) {
  if (!e.currentTarget || !(e.currentTarget as HTMLElement).contains(e.relatedTarget as Node)) isDragOver.value = false
}
function handleDrop(e: DragEvent) {
  e.preventDefault(); isDragOver.value = false
  // WebView2 can't expose File.path; Wails native drop (wails:file-drop) handles it.
  // Don't fall back to a dialog here — that's confusing.
}

function formatTime(ts: string) {
  return new Date(ts).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
}
</script>

<template>
  <div class="chat-panel" :class="{ 'drag-over': isDragOver }" @dragover="handleDragOver" @dragleave="handleDragLeave" @drop="handleDrop">
    <div ref="messagesEl" class="messages-area">
      <div v-if="props.messages.length === 0 && !isDragOver" class="empty-chat">
        <div class="empty-icon">💬</div>
        <p>{{ t('chat.emptyHint') }}</p>
        <p class="empty-drag-hint">{{ t('chat.dragHint') }}</p>
      </div>

      <div v-if="isDragOver && props.messages.length === 0" class="drop-overlay">
        <div class="drop-big-icon">📤</div>
        <p>{{ t('chat.dropHere') }}</p>
      </div>

      <div v-for="msg in props.messages" :key="msg.id" class="message" :class="msg.direction">
        <!-- File share -->
        <template v-if="tryParseShare(msg.content)">
          <div class="file-wrapper">
            <div
              class="file-card"
              :class="{ clickable: canOpenFolder(tryParseShare(msg.content), msg.direction) }"
              @click="canOpenFolder(tryParseShare(msg.content), msg.direction) && emit('openFolder', getOpenPath(tryParseShare(msg.content), msg.direction))"
            >
              <div class="file-icon">{{ msg.direction === 'sent' ? '📤' : (isDownloaded(tryParseShare(msg.content)) ? '✅' : '📄') }}</div>
              <div class="file-info">
                <div class="file-name">{{ tryParseShare(msg.content).fileName }}</div>
                <div class="file-meta">
                  {{ formatSize(tryParseShare(msg.content).fileSize) }}
                  <span v-if="canOpenFolder(tryParseShare(msg.content), msg.direction)" class="file-open-hint">· 打开所在文件夹</span>
                </div>
              </div>
            </div>
            <button
              v-if="msg.direction === 'received' && !isDownloaded(tryParseShare(msg.content))"
              class="download-dot"
              @click="emit('download', tryParseShare(msg.content))"
              title="Download"
            >↓</button>
          </div>
          <span class="msg-time">{{ formatTime(msg.time) }}</span>
        </template>

        <!-- Text bubble -->
        <template v-else>
          <div class="msg-bubble">
            <div class="msg-text">{{ msg.content }}</div>
          </div>
          <span class="msg-time">{{ formatTime(msg.time) }}</span>
        </template>
      </div>
    </div>

    <div class="chat-input-row">
      <input v-model="inputText" type="text" :placeholder="t('chat.placeholder')" @keydown="handleKeydown" class="chat-input" :disabled="props.sendingMessage" autofocus />
      <button class="btn-attach" :disabled="sending" @click="handleAttachClick" :title="t('transfer.sendFile')">📎</button>
      <button class="btn-send" :disabled="!inputText.trim() || props.sendingMessage" @click="handleSend">
        {{ props.sendingMessage ? '⏳' : t('chat.send') }}
      </button>
    </div>

    <div v-if="isDragOver && props.messages.length > 0" class="drop-overlay floating">
      <div class="drop-big-icon">📤</div>
      <p>{{ t('chat.dropHere') }}</p>
    </div>
  </div>
</template>

<style scoped>
.chat-panel { display: flex; flex-direction: column; height: 100%; position: relative; --wails-drop-target: drop; }
.chat-panel.drag-over { background: rgba(88,166,255,0.03); }

.messages-area { flex: 1; overflow-y: auto; padding: 16px 12px; display: flex; flex-direction: column; gap: 10px; }

.empty-chat { display: flex; flex-direction: column; align-items: center; justify-content: center; flex: 1; gap: 4px; color: var(--text-secondary); user-select: none; }
.empty-icon { font-size: 40px; margin-bottom: 8px; opacity: 0.5; }
.empty-chat p { font-size: 13px; }
.empty-drag-hint { font-size: 11px !important; opacity: 0.6; margin-top: 8px; }

.drop-overlay { position: absolute; inset: 0; display: flex; flex-direction: column; align-items: center; justify-content: center; background: rgba(88,166,255,0.08); border: 2px dashed var(--accent); border-radius: var(--radius); z-index: 10; pointer-events: none; }
.drop-overlay.floating { margin: 12px; border-radius: 12px; }
.drop-big-icon { font-size: 48px; margin-bottom: 8px; }
.drop-overlay p { font-size: 15px; font-weight: 500; color: var(--accent); }

/* Message rows — bubble + time on same baseline */
.message { display: flex; flex-direction: row; align-items: flex-end; gap: 5px; max-width: 82%; position: relative; }
.message.sent { align-self: flex-end; }
.message.received { align-self: flex-start; }

.msg-bubble { padding: 8px 12px; border-radius: 14px; font-size: 13px; line-height: 1.4; word-break: break-word; }
.message.sent .msg-bubble { background: var(--accent); color: #fff; border-bottom-right-radius: 4px; }
.message.received .msg-bubble { background: var(--bg-card); color: var(--text-primary); border-bottom-left-radius: 4px; }
.msg-time { font-size: 10px; opacity: 0.35; flex-shrink: 0; white-space: nowrap; padding-bottom: 3px; }

/* File message wrapper */
.file-wrapper { display: flex; flex-direction: row; align-items: center; gap: 6px; }

.file-card {
  display: flex; align-items: center; gap: 10px;
  padding: 10px 14px; border-radius: 12px; max-width: 260px;
  transition: all 0.15s;
}
.message.sent .file-card { background: var(--accent); color: #fff; border-bottom-right-radius: 4px; }
.message.received .file-card { background: var(--bg-card); color: var(--text-primary); border-bottom-left-radius: 4px; border: 1px solid var(--border); }
.file-card.clickable { cursor: pointer; }
.file-card.clickable:hover { background: var(--bg-hover); transform: translateY(-1px); }
.file-icon { font-size: 24px; flex-shrink: 0; }
.file-info { min-width: 0; }
.file-name { font-size: 13px; font-weight: 500; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.file-meta { font-size: 11px; opacity: 0.7; margin-top: 2px; }
.file-open-hint { color: var(--accent); }

/* Download dot — sits outside the card */
.download-dot {
  width: 24px; height: 24px; border-radius: 50%;
  background: var(--success); color: #fff; border: none;
  font-size: 13px; font-weight: bold; cursor: pointer; flex-shrink: 0;
  display: flex; align-items: center; justify-content: center;
  box-shadow: 0 1px 4px rgba(0,0,0,0.3);
  transition: transform 0.15s;
}
.download-dot:hover { transform: scale(1.15); }

.chat-input-row { display: flex; gap: 6px; align-items: center; padding: 10px 12px; border-top: 1px solid var(--border); flex-shrink: 0; }
.chat-input { flex: 1; padding: 8px 12px; }
.btn-attach { width: 34px; height: 34px; padding: 0; display: flex; align-items: center; justify-content: center; font-size: 16px; background: var(--bg-card); color: var(--text-secondary); border: 1px solid var(--border); border-radius: 8px; cursor: pointer; flex-shrink: 0; transition: all 0.15s; }
.btn-attach:hover:not(:disabled) { background: var(--bg-hover); color: var(--accent); border-color: var(--accent); }
.btn-attach:disabled { opacity: 0.4; cursor: not-allowed; }
.btn-send { background: var(--accent); color: #fff; padding: 8px 20px; border-radius: 8px; flex-shrink: 0; }
.btn-send:hover { background: var(--accent-hover); }
</style>
