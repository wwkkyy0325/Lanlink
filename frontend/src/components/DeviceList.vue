<script setup lang="ts">
import { ref, computed } from 'vue'
import { models } from '../../wailsjs/go/models'
import { useI18n } from '../i18n'
import { CreateGroup, JoinGroup, LeaveGroup, GetGroups, GetGroupInvite } from '../../wailsjs/go/main/App'
import { EventsOn } from '../../wailsjs/runtime/runtime'

const { t } = useI18n()

type Device = models.Device

interface Group { id: string; code: string; name: string; members: string[]; encrypted: boolean; created: string }

const props = defineProps<{
  devices: Device[]
  localDeviceId: string
  selectedDeviceId: string | null
  selectedGroupId: string | null
  unreadCounts: Record<string, number>
  groups: Group[]
  isOnline: boolean
  isToggling: boolean
}>()

const emit = defineEmits<{
  select: [device: Device]
  selectGroup: [group: Group]
  groupsChanged: []
  toggleOnline: []
  refresh: []
  openConnect: []
  removeDevice: [id: string]
}>()

const showCreate = ref(false)
const showJoin = ref(false)
const showGroupMenu = ref(false)
const newGroupName = ref('')
const joinCode = ref('')
const inviteCode = ref('')
const lastCreatedGroupId = ref('')

const localDevice = computed(() => props.devices.find(d => d.id === props.localDeviceId))

const devicesByGroup = computed(() => {
  const map: Record<string, Device[]> = {}
  for (const d of props.devices) {
    if (d.id === props.localDeviceId) continue
    if (!d.online) continue
    for (const gid of d.groups || []) {
      if (!map[gid]) map[gid] = []
      if (!map[gid].find(x => x.id === d.id)) map[gid].push(d)
    }
  }
  return map
})

const ungroupedDevices = computed(() => props.devices.filter(d => {
  if (d.id === props.localDeviceId) return false
  if (!d.online) return false
  return (d.groups || []).length === 0
}))

const offlineDevices = computed(() => props.devices.filter(d => {
  if (d.id === props.localDeviceId) return false
  return !d.online
}))

function selectDevice(d: Device) {
  if (d.id === props.localDeviceId) return
  emit('select', d)
}

function unreadCount(d: Device): number {
  return props.unreadCounts[d.id] || 0
}

function copyToClipboard(text: string) {
  navigator.clipboard.writeText(text).catch(() => {})
}

async function handleCreate() {
  const name = newGroupName.value.trim()
  if (!name) return
  const group = await CreateGroup(name)
  if (group) {
    lastCreatedGroupId.value = group.id
    inviteCode.value = await GetGroupInvite(group.id)
  }
  newGroupName.value = ''
  emit('groupsChanged')
}

async function handleJoin() {
  const input = joinCode.value.trim()
  if (!input || input.length < 4) return
  await JoinGroup(input)
  joinCode.value = ''
  showJoin.value = false
  inviteCode.value = ''
  emit('groupsChanged')
}

async function handleLeave(gid: string) {
  await LeaveGroup(gid)
  emit('groupsChanged')
}
</script>

<template>
  <div class="device-list">
    <div class="section-title">{{ t('device.myDevice') }}</div>
    <div v-if="localDevice" class="device-card local-card">
      <div class="device-icon">🖥️</div>
      <div class="device-info">
        <div class="device-name">{{ localDevice.name }}</div>
        <div class="device-ip">
          {{ localDevice.ip }}
          · <span v-if="props.isOnline" class="status-text online-tag">🟢 Online</span>
          <span v-else class="status-text offline-tag">⚫ Offline</span>
        </div>
      </div>
      <button class="btn-toggle-online" :class="{ loading: props.isToggling }" @click="emit('toggleOnline')" :disabled="props.isToggling">
        {{ props.isToggling ? '⏳' : (props.isOnline ? '⚫' : '🟢') }}
      </button>
    </div>

    <div class="section-title">
      👥 {{ t('group.myGroups') }}
      <span class="section-actions">
        <button class="btn-tiny" @click="emit('openConnect')" title="P2P">🌐</button>
        <div class="dropdown-wrap">
          <button class="btn-tiny" @click="showGroupMenu = !showGroupMenu" title="Groups">＋▾</button>
          <div v-if="showGroupMenu" class="dropdown-menu" @click="showGroupMenu = false">
            <button class="dropdown-item" @click="showCreate = true">＋ {{ t('group.createGroup') }}</button>
            <button class="dropdown-item" @click="showJoin = true">🔗 {{ t('group.joinGroup') }}</button>
          </div>
        </div>
        <button class="btn-tiny refresh" @click="emit('refresh')">🔄</button>
      </span>
    </div>

    <div v-if="props.groups.length === 0 && ungroupedDevices.length === 0" class="empty-hint">
      <p>{{ t('group.noGroups') }}</p>
    </div>

    <div v-for="g in props.groups" :key="g.id" class="group-block">
      <div class="group-header" :class="{ selected: props.selectedGroupId === g.id }" @click="emit('selectGroup', g)">
        <span class="group-icon">{{ g.encrypted ? '🔒' : '👥' }}</span>
        <span class="group-name">{{ g.name }}</span>
        <span class="group-code">#{{ g.code }}</span>
        <span class="group-count">{{ (devicesByGroup[g.id] || []).length }}</span>
        <button class="btn-leave-mini" @click.stop="handleLeave(g.id)">✕</button>
      </div>
      <div v-for="d in (devicesByGroup[g.id] || [])" :key="d.id" class="device-card sub" :class="{ selected: props.selectedDeviceId === d.id }" @click="selectDevice(d)">
        <div class="device-icon sub-icon">
          <span class="status-dot" :class="d.online ? 'online' : 'offline'"></span>
          {{ d.source === 'p2p' ? '🌐' : '💻' }}
        </div>
        <div class="device-info">
          <div class="device-name">{{ d.name || d.id.slice(0, 10) }}</div>
          <div class="device-ip">{{ d.ip || d.id.slice(0, 14) + '...' }}</div>
        </div>
        <div v-if="unreadCount(d) > 0" class="unread-badge">{{ unreadCount(d) > 99 ? '99+' : unreadCount(d) }}</div>
      </div>
    </div>

    <template v-if="ungroupedDevices.length > 0">
      <div class="section-title">
        💻 {{ t('device.onlineDevices') }}
        <span class="badge">{{ ungroupedDevices.length }}</span>
      </div>
      <div v-for="d in ungroupedDevices" :key="d.id" class="device-card" :class="{ selected: props.selectedDeviceId === d.id }" @click="selectDevice(d)">
        <div class="device-icon"><span class="status-dot online"></span>{{ d.source === 'manual' ? '🛡' : '💻' }}</div>
        <div class="device-info">
          <div class="device-name">{{ d.name || d.id.slice(0, 10) }}</div>
          <div class="device-ip">{{ d.ip || d.id.slice(0, 14) + '...' }}</div>
        </div>
        <div v-if="unreadCount(d) > 0" class="unread-badge">{{ unreadCount(d) > 99 ? '99+' : unreadCount(d) }}</div>
        <button v-if="d.source === 'manual'" class="btn-leave-mini always-show" @click.stop="emit('removeDevice', d.id)" title="Remove">✕</button>
      </div>
    </template>

    <template v-if="offlineDevices.length > 0">
      <div class="section-title dim">{{ t('device.offline') }}</div>
      <div v-for="d in offlineDevices" :key="d.id" class="device-card offline-clickable" :class="{ selected: props.selectedDeviceId === d.id }" @click="selectDevice(d)">
        <div class="device-icon"><span class="status-dot offline"></span>💻</div>
        <div class="device-info">
          <div class="device-name">{{ d.name || d.id.slice(0, 10) }}</div>
          <div class="device-ip">{{ t('device.offline') }}</div>
        </div>
      </div>
    </template>

    <!-- Create Group Modal -->
    <div v-if="showCreate" class="modal-overlay" @click.self="showCreate = false">
      <div class="modal">
        <div class="modal-title">🔒 {{ t('group.createGroup') }}</div>
        <div v-if="!inviteCode">
          <div class="modal-hint">{{ t('group.createHint') }}</div>
          <input v-model="newGroupName" :placeholder="t('group.namePlaceholder')" class="modal-input" @keydown.enter="handleCreate" />
          <div class="modal-btns">
            <button class="btn-cancel" @click="showCreate = false">✕</button>
            <button class="btn-confirm" :disabled="!newGroupName.trim()" @click="handleCreate">{{ t('group.create') }}</button>
          </div>
        </div>
        <div v-else>
          <div class="modal-hint">Share this invite code:</div>
          <div class="invite-code-box"><code>{{ inviteCode }}</code></div>
          <button class="btn-confirm full" @click="copyToClipboard(inviteCode); showCreate = false">📋 Copy &amp; Close</button>
        </div>
      </div>
    </div>

    <div v-if="showJoin" class="modal-overlay" @click.self="showJoin = false">
      <div class="modal">
        <div class="modal-title">🔑 {{ t('group.joinGroup') }}</div>
        <div class="modal-hint">{{ t('group.joinHint') }}<br>Paste the invite code:</div>
        <input v-model="joinCode" placeholder="Paste invite code..." class="modal-input" @keydown.enter="handleJoin" />
        <div class="modal-btns">
          <button class="btn-cancel" @click="showJoin = false">✕</button>
          <button class="btn-confirm" :disabled="joinCode.trim().length < 4" @click="handleJoin">{{ t('group.join') }}</button>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.device-list { padding: 8px; }

.section-title {
  font-size: 11px; font-weight: 600; text-transform: uppercase;
  color: var(--text-secondary); padding: 12px 12px 4px;
  display: flex; align-items: center; gap: 6px; user-select: none;
}
.section-title.dim { opacity: 0.6; }
.section-actions { margin-left: auto; display: flex; gap: 2px; }
.btn-tiny {
  padding: 1px 6px; font-size: 11px; background: var(--bg-card);
  color: var(--accent); border: 1px solid var(--border); border-radius: 3px;
  cursor: pointer; line-height: 1.4;
}
.btn-tiny:hover { background: var(--accent); color: #fff; border-color: var(--accent); }
.btn-tiny.refresh { color: var(--text-secondary); }
.btn-tiny.refresh:hover { background: var(--bg-hover); color: var(--text-primary); }

.dropdown-wrap { position: relative; }
.dropdown-menu {
  position: absolute; right: 0; top: 100%; margin-top: 2px;
  background: var(--bg-secondary); border: 1px solid var(--border);
  border-radius: 6px; box-shadow: 0 4px 12px rgba(0,0,0,0.3);
  z-index: 50; min-width: 140px; overflow: hidden;
}
.dropdown-item {
  display: block; width: 100%; text-align: left; padding: 8px 12px;
  font-size: 12px; background: transparent; color: var(--text-primary);
  border: none; cursor: pointer; white-space: nowrap;
}
.dropdown-item:hover { background: var(--accent); color: #fff; }
.badge { background: var(--accent); color: #fff; font-size: 10px; padding: 1px 6px; border-radius: 10px; }

.device-card {
  display: flex; align-items: center; gap: 10px;
  padding: 8px 10px; border-radius: var(--radius); margin: 1px 0;
  cursor: pointer; transition: background 0.12s;
}
.device-card:hover { background: var(--bg-hover); }
.device-card.selected { background: var(--accent); color: #fff; }
.device-card.selected .device-ip { color: rgba(255,255,255,0.7); }
.device-card.sub { padding-left: 24px; }
.device-card.offline { opacity: 0.5; }
.device-card.offline-clickable { cursor: pointer; opacity: 0.55; }
.device-card.offline-clickable:hover { background: var(--bg-hover); opacity: 0.8; }
.local-card { background: var(--bg-card); border: 1px dashed var(--border); cursor: default; }
.local-card:hover { background: var(--bg-card); }

.device-icon { font-size: 20px; position: relative; flex-shrink: 0; }
.sub-icon { font-size: 16px; }
.status-dot {
  position: absolute; bottom: -1px; right: -1px; width: 7px; height: 7px;
  border-radius: 50%; border: 2px solid var(--bg-primary);
}
.status-dot.online { background: var(--success); }
.status-dot.offline { background: var(--text-secondary); }
.status-text { font-size: 10px; font-weight: 500; }
.online-tag { color: var(--success); }
.offline-tag { color: var(--text-secondary); }

.device-info { flex: 1; min-width: 0; }
.device-name { font-size: 13px; font-weight: 500; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.device-ip { font-size: 11px; color: var(--text-secondary); margin-top: 1px; }

.unread-badge {
  background: var(--danger); color: #fff; font-size: 10px; font-weight: 700;
  min-width: 18px; height: 18px; border-radius: 9px;
  display: flex; align-items: center; justify-content: center;
  flex-shrink: 0; padding: 0 5px;
}

.btn-toggle-online {
  padding: 2px 8px; font-size: 14px; background: transparent;
  border: 1px solid var(--border); border-radius: 4px; cursor: pointer; flex-shrink: 0;
  line-height: 1; transition: all 0.15s;
}
.btn-toggle-online:hover { background: var(--bg-hover); border-color: var(--text-secondary); }
.btn-toggle-online:disabled { cursor: wait; opacity: 0.6; }
.btn-toggle-online.loading { animation: pulse 1s infinite; }
@keyframes pulse { 0%, 100% { opacity: 1; } 50% { opacity: 0.4; } }

.group-block { margin: 2px 0; }
.group-header {
  display: flex; align-items: center; gap: 6px;
  padding: 8px 10px; border-radius: 6px; cursor: pointer;
  background: var(--bg-card); transition: background 0.12s;
}
.group-header:hover { background: var(--bg-hover); }
.group-header.selected { background: var(--accent); color: #fff; }
.group-header.selected .group-code { color: rgba(255,255,255,0.6); }
.group-icon { font-size: 14px; }
.group-name { font-size: 12px; font-weight: 600; flex: 1; }
.group-code { font-size: 10px; color: var(--text-secondary); font-family: monospace; }
.group-count { font-size: 10px; color: var(--text-secondary); background: var(--bg-primary); padding: 1px 6px; border-radius: 8px; }
.btn-leave-mini {
  background: transparent; color: var(--danger); border: none;
  font-size: 10px; cursor: pointer; padding: 2px;
  opacity: 0; transition: opacity 0.12s;
}
.group-header:hover .btn-leave-mini { opacity: 1; }
.btn-leave-mini.always-show { opacity: 0.6; }

.empty-hint { padding: 16px 12px; text-align: center; color: var(--text-secondary); font-size: 11px; }

.invite-code-box {
  background: var(--bg-primary); border: 1px solid var(--border); border-radius: 6px;
  padding: 10px 12px; margin: 8px 0; word-break: break-all;
}
.invite-code-box code { font-size: 13px; color: var(--accent); letter-spacing: 1px; }

.modal-overlay { position: fixed; inset: 0; background: rgba(0,0,0,0.5); display: flex; align-items: center; justify-content: center; z-index: 100; }
.modal { background: var(--bg-secondary); border: 1px solid var(--border); border-radius: var(--radius); padding: 20px; width: 300px; box-shadow: var(--shadow); }
.modal-title { font-size: 15px; font-weight: 600; margin-bottom: 4px; }
.modal-hint { font-size: 11px; color: var(--text-secondary); margin-bottom: 12px; line-height: 1.5; }
.modal-input { width: 100%; padding: 8px 10px; margin-bottom: 12px; font-size: 14px; border: 1px solid var(--border); border-radius: 6px; background: var(--bg-primary); color: var(--text-primary); box-sizing: border-box; }
.modal-btns { display: flex; gap: 8px; justify-content: flex-end; }
.btn-cancel { padding: 6px 14px; background: transparent; color: var(--text-secondary); border: 1px solid var(--border); border-radius: 4px; cursor: pointer; }
.btn-confirm { padding: 6px 18px; background: var(--accent); color: #fff; border: none; border-radius: 4px; cursor: pointer; font-weight: 500; }
.btn-confirm:disabled { opacity: 0.5; cursor: not-allowed; }
.btn-confirm.full { width: 100%; margin-top: 8px; padding: 10px; }
</style>
