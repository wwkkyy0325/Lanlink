package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"time"

	"lanlink/crypto"
	"lanlink/models"
	"lanlink/transfer"

	"github.com/google/uuid"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// ============================================================
// Group CRUD
// ============================================================

func (a *App) CreateGroup(name string) *models.Group {
	code := generateGroupCode()
	key := crypto.GenerateGroupKey()
	group := models.Group{
		ID:        uuid.NewString(),
		Code:      code,
		Name:      name,
		Members:   []string{a.getLocalDevice().ID},
		Encrypted: true,
		Key:       key,
		Created:   time.Now(),
	}
	a.groups = append(a.groups, group)
	a.syncGroupIDs()
	a.emitGroupUpdate()
	return &group
}

func (a *App) JoinGroup(invite string) *models.Group {
	code, key, ok := crypto.ParseInvite(invite)
	if !ok {
		code = invite // backward compat: plain 6-char code
		key = crypto.DeriveKey(code)
	}

	// Already joined?
	for i := range a.groups {
		if a.groups[i].Code == code {
			// Update key if we have a better one
			if key != "" && a.groups[i].Key == "" {
				a.groups[i].Key = key
			}
			return &a.groups[i]
		}
	}

	group := models.Group{
		ID:        uuid.NewString(),
		Code:      code,
		Name:      "Group " + code,
		Members:   []string{a.getLocalDevice().ID},
		Encrypted: true,
		Key:       key,
		Created:   time.Now(),
	}
	a.groups = append(a.groups, group)
	a.syncGroupIDs()
	a.emitGroupUpdate()
	return &group
}

// GetGroupInvite returns the shareable invite string for a group (code:key)
func (a *App) GetGroupInvite(groupID string) string {
	for _, g := range a.groups {
		if g.ID == groupID {
			return crypto.CreateInvite(g.Code, g.Key)
		}
	}
	return ""
}

func (a *App) LeaveGroup(groupID string) {
	newGroups := make([]models.Group, 0)
	for _, g := range a.groups {
		if g.ID != groupID {
			newGroups = append(newGroups, g)
		}
	}
	a.groups = newGroups
	a.syncGroupIDs()
	a.emitGroupUpdate()
}

func (a *App) GetGroups() []models.Group {
	result := make([]models.Group, len(a.groups))
	copy(result, a.groups)
	devices := a.GetDevices()
	localID := a.getLocalDevice().ID
	for gi := range result {
		members := []string{localID}
		for _, d := range devices {
			if d.ID == localID {
				continue
			}
			for _, gid := range d.Groups {
				if gid == result[gi].ID {
					members = append(members, d.ID)
					break
				}
			}
		}
		result[gi].Members = members
	}
	return result
}

// ============================================================
// File Confirmation
// ============================================================

func (a *App) RespondTransfer(requestID string, accepted bool) {
	if a.transferServer != nil {
		a.transferServer.RespondToRequest(requestID, accepted)
	}
}

// ============================================================
// Group Broadcast
// ============================================================

func (a *App) SendGroupMessage(groupID string, content string) *models.Message {
	// Find group and pick encryption key
	var group models.Group
	found := false
	for _, g := range a.groups {
		if g.ID == groupID {
			group = g
			found = true
			break
		}
	}

	sendContent := content
	if found && group.Encrypted {
		key := group.Key
		if key == "" {
			key = crypto.DeriveKey(group.Code) // backward compat
		}
		sendContent = crypto.EncryptMessage(content, key)
	}

	localID := a.getLocalDevice().ID
	for _, d := range a.GetDevices() {
		if d.ID == localID || !d.Online {
			continue
		}
		for _, gid := range d.Groups {
			if gid == groupID {
				if d.Source == "p2p" && a.p2pNode != nil {
					a.p2pNode.SendP2PMessage(d.ID, sendContent, a.getLocalDevice())
				} else if d.IP != "" {
					transfer.SendMessage(d.IP, transferPort, d.ID, d.Name, sendContent, a.getLocalDevice())
				}
				break
			}
		}
	}
	msg := models.Message{
		ID:         uuid.NewString(),
		DeviceID:   groupID,
		DeviceName: "Group",
		Content:    content,
		Time:       time.Now(),
		Direction:  "sent",
	}
	a.messages = append(a.messages, msg)
	runtime.EventsEmit(a.ctx, "message-sent", msg)
	return &msg
}

func (a *App) SendGroupFile(groupID string) []models.TransferRecord {
	filePath, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{Title: "Select File to Send"})
	if err != nil || filePath == "" {
		return nil
	}
	return a.sendGroupFile(groupID, filePath)
}

func (a *App) SendGroupFilePath(groupID string, filePath string) []models.TransferRecord {
	return a.sendGroupFile(groupID, filePath)
}

func (a *App) sendGroupFile(groupID string, filePath string) []models.TransferRecord {
	localID := a.getLocalDevice().ID
	var records []models.TransferRecord
	for _, d := range a.GetDevices() {
		if d.ID == localID || !d.Online {
			continue
		}
		for _, gid := range d.Groups {
			if gid == groupID {
				if d.Source == "p2p" && a.p2pNode != nil {
					r, _ := a.p2pNode.SendP2PFile(d.ID, filePath, a.getLocalDevice())
					if r != nil {
						records = append(records, *r)
					}
				} else if d.IP != "" {
					r, _ := transfer.SendFile(d.IP, transferPort, d.ID, d.Name, filePath, a.getLocalDevice())
					if r != nil {
						records = append(records, *r)
					}
				}
				break
			}
		}
	}
	return records
}

// ============================================================
// Helpers
// ============================================================

func (a *App) syncGroupIDs() {
	gids := make([]string, 0, len(a.groups))
	for _, g := range a.groups {
		gids = append(gids, g.ID)
	}
	if a.discovery != nil {
		a.discovery.SetGroupIDs(gids)
	}
	runtime.EventsEmit(a.ctx, "devices-changed", a.GetDevices())
}

func (a *App) emitGroupUpdate() {
	runtime.EventsEmit(a.ctx, "groups-changed", a.GetGroups())
}

func (a *App) RefreshDevices() []models.Device {
	runtime.EventsEmit(a.ctx, "devices-changed", a.GetDevices())
	return a.GetDevices()
}

func generateGroupCode() string {
	b := make([]byte, 3)
	rand.Read(b)
	return hex.EncodeToString(b)[:6]
}

// ShareFile shares a local file (makes it available for download) and sends
// a share message to the target device. Returns the share info.
func (a *App) ShareFile(deviceIP string, deviceID string, deviceName string) *models.SharedFile {
	filePath, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{Title: "Select File to Share"})
	if err != nil || filePath == "" {
		return nil
	}
	return a.shareFile(deviceIP, deviceID, deviceName, filePath)
}

func (a *App) ShareFilePath(deviceIP string, deviceID string, deviceName string, filePath string) *models.SharedFile {
	return a.shareFile(deviceIP, deviceID, deviceName, filePath)
}

func (a *App) shareFile(deviceIP string, deviceID string, deviceName string, filePath string) *models.SharedFile {
	if a.transferServer == nil {
		return nil
	}
	sf := a.transferServer.ShareFile(filePath, a.getLocalDevice())

	// Build share info as JSON message content
	type shareMsg struct {
		Type       string `json:"type"`
		ShareID    string `json:"shareId"`
		FileName   string `json:"fileName"`
		FileSize   int64  `json:"fileSize"`
		SenderIP   string `json:"senderIP"`
		SenderName string `json:"senderName"`
		SenderPath string `json:"senderPath,omitempty"` // sender's local path (only useful to sender)
	}
	content, _ := json.Marshal(shareMsg{
		Type: "share", ShareID: sf.ShareID,
		FileName: sf.FileName, FileSize: sf.FileSize,
		SenderIP: sf.SenderIP, SenderName: sf.SenderName,
		SenderPath: filePath,
	})
	contentStr := string(content)

	// Send to remote (best-effort, log errors)
	if _, err := transfer.SendMessage(deviceIP, transferPort, deviceID, deviceName, contentStr, a.getLocalDevice()); err != nil {
		runtime.LogErrorf(a.ctx, "ShareFile send failed: %v", err)
	}

	// Record locally so sender sees the file card immediately
	msg := models.Message{
		ID:         uuid.NewString(),
		DeviceID:   deviceID,
		DeviceName: deviceName,
		Content:    contentStr,
		Time:       time.Now(),
		Direction:  "sent",
	}
	a.messages = append(a.messages, msg)
	a.saveChatData()
	runtime.EventsEmit(a.ctx, "message-sent", msg)

	return sf
}

// ShareGroupFile shares a file to all online members of a group
func (a *App) ShareGroupFile(groupID string) []*models.SharedFile {
	filePath, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{Title: "Select File to Share"})
	if err != nil || filePath == "" {
		return nil
	}
	return a.shareGroupFile(groupID, filePath)
}

func (a *App) ShareGroupFilePath(groupID string, filePath string) []*models.SharedFile {
	return a.shareGroupFile(groupID, filePath)
}

func (a *App) shareGroupFile(groupID string, filePath string) []*models.SharedFile {
	if a.transferServer == nil {
		return nil
	}
	sf := a.transferServer.ShareFile(filePath, a.getLocalDevice())
	var results []*models.SharedFile

	type shareMsg struct {
		Type       string `json:"type"`
		ShareID    string `json:"shareId"`
		FileName   string `json:"fileName"`
		FileSize   int64  `json:"fileSize"`
		SenderIP   string `json:"senderIP"`
		SenderName string `json:"senderName"`
		SenderPath string `json:"senderPath,omitempty"`
	}
	content, _ := json.Marshal(shareMsg{
		Type: "share", ShareID: sf.ShareID,
		FileName: sf.FileName, FileSize: sf.FileSize,
		SenderIP: sf.SenderIP, SenderName: sf.SenderName,
		SenderPath: filePath,
	})

	localID := a.getLocalDevice().ID
	sent := false
	for _, d := range a.GetDevices() {
		if d.ID == localID || !d.Online {
			continue
		}
		for _, gid := range d.Groups {
			if gid == groupID {
				if d.Source == "p2p" && a.p2pNode != nil {
					a.p2pNode.SendP2PMessage(d.ID, string(content), a.getLocalDevice())
					sent = true
				} else if d.IP != "" {
					if _, err := transfer.SendMessage(d.IP, transferPort, d.ID, d.Name, string(content), a.getLocalDevice()); err != nil {
						runtime.LogErrorf(a.ctx, "group share to %s failed: %v", d.Name, err)
					} else {
						sent = true
					}
				}
				break
			}
		}
		results = append(results, sf)
	}

	// Record locally so sender sees the file card
	if sent || true {
		msg := models.Message{
			ID:         uuid.NewString(),
			DeviceID:   groupID,
			DeviceName: "Group",
			Content:    string(content),
			Time:       time.Now(),
			Direction:  "sent",
		}
		a.messages = append(a.messages, msg)
		a.saveChatData()
		runtime.EventsEmit(a.ctx, "message-sent", msg)
	}

	return results
}

// DownloadSharedFile downloads a shared file. If askSaveLocation is on, prompts
// the user for a save path; otherwise saves to downloadDir.
// Returns the full save path on success, or an error message otherwise.
func (a *App) DownloadSharedFile(deviceIP string, shareID string, fileName string) string {
	url := fmt.Sprintf("http://%s:%d/download/%s", deviceIP, transferPort, shareID)
	resp, err := http.Get(url)
	if err != nil {
		return "download failed: " + err.Error()
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "download failed: HTTP " + resp.Status
	}

	// Decide save path
	savePath := filepath.Join(a.downloadDir, fileName)
	if a.askSaveLocation {
		savePath, err = runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
			Title:           "Save File",
			DefaultFilename: fileName,
		})
		if err != nil || savePath == "" {
			resp.Body.Close()
			return "cancelled"
		}
	} else {
		savePath = uniqueFilePath(savePath)
	}

	out, err := os.Create(savePath)
	if err != nil {
		return "save failed: " + err.Error()
	}
	defer out.Close()

	written, _ := io.Copy(out, resp.Body)

	record := models.TransferRecord{
		ID:         uuid.NewString(),
		DeviceName: "Remote",
		FileName:   filepath.Base(savePath),
		FileSize:   written,
		Direction:  "received",
		Status:     "completed",
		Time:       time.Now(),
	}
	a.history = append(a.history, record)
	a.saveChatData()
	runtime.EventsEmit(a.ctx, "file-received", record)
	return savePath
}

// OpenFileInFolder opens the file's containing folder in the OS file manager.
// Returns "ok" or an error message.
func (a *App) OpenFileInFolder(filePath string) string {
	if filePath == "" {
		return "empty path"
	}
	dir := filepath.Dir(filePath)
	var cmd *exec.Cmd
	switch goruntime.GOOS {
	case "windows":
		cmd = exec.Command("explorer", dir)
	case "darwin":
		cmd = exec.Command("open", dir)
	default:
		cmd = exec.Command("xdg-open", dir)
	}
	if err := cmd.Start(); err != nil {
		log.Printf("[OpenFileInFolder] failed: %v (dir=%s)", err, dir)
		return err.Error()
	}
	return "ok"
}

// OpenFile opens a file directly with its default app.
func (a *App) OpenFile(filePath string) string {
	openInOS(filePath)
	return "ok"
}

// openInOS opens a path with the OS-default handler (cross-platform).
func openInOS(target string) {
	var cmd *exec.Cmd
	switch goruntime.GOOS {
	case "windows":
		cmd = exec.Command("explorer", target)
	case "darwin":
		cmd = exec.Command("open", target)
	default: // linux, bsd, etc.
		cmd = exec.Command("xdg-open", target)
	}
	cmd.Start()
}

func uniqueFilePath(path string) string {
	dir := filepath.Dir(path)
	ext := filepath.Ext(path)
	base := filepath.Base(path)
	name := base[:len(base)-len(ext)]
	for i := 1; ; i++ {
		if _, err := os.Stat(path); os.IsNotExist(err) {
			return path
		}
		path = filepath.Join(dir, fmt.Sprintf("%s (%d)%s", name, i, ext))
	}
}

func groupIDs(groups []models.Group) []string {
	ids := make([]string, 0, len(groups))
	for _, g := range groups {
		ids = append(ids, g.ID)
	}
	return ids
}
