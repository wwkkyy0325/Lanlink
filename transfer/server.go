package transfer

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"lanlink/models"

	"github.com/google/uuid"
)

// Server handles incoming file transfers and messages
type Server struct {
	mu              sync.RWMutex
	port            int
	downloadDir     string
	history         []models.TransferRecord
	messages        []models.Message
	pendingRequests map[string]chan bool
	sharedFiles     map[string]*models.SharedFile // shareID → file info
	httpServer      *http.Server
	onFileReceived  func(record models.TransferRecord)
	onMessage       func(msg models.Message)
	onTransferReq   func(req models.TransferRequest)
	localDevice     func() models.Device
}

// NewServer creates a transfer server
func NewServer(port int, downloadDir string, localDevice func() models.Device,
	onFile func(models.TransferRecord), onMsg func(models.Message),
	onTransferReq func(models.TransferRequest)) *Server {

	os.MkdirAll(downloadDir, 0755)
	return &Server{
		port:            port,
		downloadDir:     downloadDir,
		pendingRequests: make(map[string]chan bool),
		sharedFiles:     make(map[string]*models.SharedFile),
		onFileReceived:  onFile,
		onMessage:       onMsg,
		onTransferReq:   onTransferReq,
		localDevice:     localDevice,
	}
}

// Start begins the HTTP server
func (s *Server) Start() error {
	mux := http.NewServeMux()
	mux.HandleFunc("/upload", s.handleUpload)
	mux.HandleFunc("/request", s.handleRequest)
	mux.HandleFunc("/download/", s.handleDownload)
	mux.HandleFunc("/message", s.handleMessage)
	mux.HandleFunc("/info", s.handleInfo)
	mux.HandleFunc("/ping", s.handlePing)

	s.httpServer = &http.Server{Addr: fmt.Sprintf(":%d", s.port), Handler: mux}
	listener, err := net.Listen("tcp", s.httpServer.Addr)
	if err != nil {
		return err
	}
	go s.httpServer.Serve(listener)
	return nil
}

// Stop shuts down the transfer server
func (s *Server) Stop() {
	if s.httpServer != nil {
		s.httpServer.Close()
	}
}

// RespondToRequest is called by frontend when user accepts/rejects a transfer
func (s *Server) RespondToRequest(requestID string, accepted bool) {
	s.mu.Lock()
	ch, ok := s.pendingRequests[requestID]
	s.mu.Unlock()
	if ok {
		ch <- accepted
	}
}

// GetHistory returns transfer history
func (s *Server) GetHistory() []models.TransferRecord {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]models.TransferRecord, len(s.history))
	copy(result, s.history)
	return result
}

// GetMessages returns message history
func (s *Server) GetMessages() []models.Message {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]models.Message, len(s.messages))
	copy(result, s.messages)
	return result
}

// ---- HTTP handlers ----

// ShareFile registers a local file for sharing and returns its share ID
func (s *Server) ShareFile(filePath string, sender models.Device) *models.SharedFile {
	sf := &models.SharedFile{
		ID:         uuid.NewString(),
		ShareID:    uuid.NewString()[:8],
		FileName:   filepath.Base(filePath),
		FilePath:   filePath,
		FileSize:   fileSize(filePath),
		SenderID:   sender.ID,
		SenderIP:   sender.IP,
		SenderName: sender.Name,
	}
	s.mu.Lock()
	s.sharedFiles[sf.ShareID] = sf
	s.mu.Unlock()
	return sf
}

// handleDownload serves a shared file for download: GET /download/{shareID}
func (s *Server) handleDownload(w http.ResponseWriter, r *http.Request) {
	shareID := r.URL.Path[len("/download/"):]
	if shareID == "" {
		http.Error(w, "missing share ID", http.StatusBadRequest)
		return
	}
	s.mu.RLock()
	sf, ok := s.sharedFiles[shareID]
	s.mu.RUnlock()
	if !ok {
		http.Error(w, "share not found", http.StatusNotFound)
		return
	}

	f, err := os.Open(sf.FilePath)
	if err != nil {
		http.Error(w, "file error", http.StatusInternalServerError)
		return
	}
	defer f.Close()
	stat, _ := f.Stat()
	w.Header().Set("Accept-Ranges", "bytes")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, sf.FileName))
	w.Header().Set("Content-Type", "application/octet-stream")
	rangeH := r.Header.Get("Range")
	if rangeH == "" {
		http.ServeContent(w, r, sf.FileName, stat.ModTime(), f)
		return
	}
	var start int64
	if n, _ := fmt.Sscanf(rangeH, "bytes=%d-", &start); n != 1 || start >= stat.Size() {
		http.Error(w, "bad range", http.StatusRequestedRangeNotSatisfiable)
		return
	}
	f.Seek(start, 0)
	w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, stat.Size()-1, stat.Size()))
	w.WriteHeader(http.StatusPartialContent)
	io.CopyN(w, f, stat.Size()-start)
}

// handleRequest: sender asks permission before sending file data
func (s *Server) handleRequest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req models.TransferRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	reqID := uuid.NewString()
	req.ID = reqID

	// Create a channel to wait for user response
	respCh := make(chan bool, 1)
	s.mu.Lock()
	s.pendingRequests[reqID] = respCh
	s.mu.Unlock()

	// Notify frontend
	if s.onTransferReq != nil {
		s.onTransferReq(req)
	}

	// Wait for user response (with 60s timeout)
	select {
	case accepted := <-respCh:
		s.mu.Lock()
		delete(s.pendingRequests, reqID)
		s.mu.Unlock()

		resp := models.TransferResponse{RequestID: reqID, Accepted: accepted}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)

	case <-time.After(15 * time.Second):
		s.mu.Lock()
		delete(s.pendingRequests, reqID)
		s.mu.Unlock()
		http.Error(w, "request timed out", http.StatusRequestTimeout)
	}
}

// handleUpload: receive file data (called after request is accepted)
func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if err := r.ParseMultipartForm(2 << 30); err != nil {
		http.Error(w, "failed to parse form: "+err.Error(), http.StatusBadRequest)
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "failed to read file: "+err.Error(), http.StatusBadRequest)
		return
	}
	defer file.Close()

	senderID := r.FormValue("senderId")
	senderName := r.FormValue("senderName")

	savePath := uniquePath(filepath.Join(s.downloadDir, header.Filename))
	out, err := os.Create(savePath)
	if err != nil {
		http.Error(w, "failed to create file: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer out.Close()

	written, err := io.Copy(out, file)
	if err != nil {
		http.Error(w, "failed to save file: "+err.Error(), http.StatusInternalServerError)
		return
	}

	record := models.TransferRecord{
		ID:         uuid.NewString(),
		DeviceID:   senderID,
		DeviceName: senderName,
		FileName:   header.Filename,
		FileSize:   written,
		Direction:  "received",
		Status:     "completed",
		Time:       time.Now(),
	}

	s.mu.Lock()
	s.history = append(s.history, record)
	s.mu.Unlock()

	if s.onFileReceived != nil {
		s.onFileReceived(record)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (s *Server) handleMessage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var body struct {
		Content    string `json:"content"`
		SenderID   string `json:"senderId"`
		SenderName string `json:"senderName"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}

	// Override senderIP in share messages with the actual TCP source address.
	// This fixes cross-network scenarios (e.g. Radmin VPN): the sender's real
	// LAN IP is unreachable, but the TCP connection's remote addr is the
	// correctly-routed IP (Radmin virtual IP if via Radmin, real IP if LAN).
	sourceIP, _, _ := net.SplitHostPort(r.RemoteAddr)
	body.Content = overrideShareIP(body.Content, sourceIP)

	msg := models.Message{
		ID:         uuid.NewString(),
		DeviceID:   body.SenderID,
		DeviceName: body.SenderName,
		Content:    body.Content,
		Time:       time.Now(),
		Direction:  "received",
	}

	s.mu.Lock()
	s.messages = append(s.messages, msg)
	s.mu.Unlock()

	if s.onMessage != nil {
		s.onMessage(msg)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok", "id": msg.ID})
}

func (s *Server) handleInfo(w http.ResponseWriter, r *http.Request) {
	device := s.localDevice()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(device)
}

func (s *Server) handlePing(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("pong"))
}

func fileSize(path string) int64 {
	info, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return info.Size()
}

func uniquePath(path string) string {
	dir := filepath.Dir(path)
	ext := filepath.Ext(path)
	name := filepath.Base(path)
	nameWithoutExt := name[:len(name)-len(ext)]
	for i := 1; ; i++ {
		if _, err := os.Stat(path); os.IsNotExist(err) {
			return path
		}
		path = filepath.Join(dir, fmt.Sprintf("%s (%d)%s", nameWithoutExt, i, ext))
	}
}

// overrideShareIP replaces senderIP inside a share JSON message with the given IP.
// If the content isn't a share message, it's returned unchanged.
func overrideShareIP(content string, ip string) string {
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(content), &m); err != nil {
		return content
	}
	if t, ok := m["type"].(string); !ok || t != "share" {
		return content
	}
	m["senderIP"] = ip
	data, _ := json.Marshal(m)
	return string(data)
}
