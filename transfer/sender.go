package transfer

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"lanlink/models"

	"github.com/google/uuid"
)

// SendFile sends a file to a remote device: request → wait confirm → upload
func SendFile(deviceIP string, devicePort int, deviceID string, deviceName string, filePath string, localDevice models.Device) (*models.TransferRecord, error) {
	fileName := filepath.Base(filePath)
	fileSize := getFileSize(filePath)

	record := &models.TransferRecord{
		ID:         uuid.NewString(),
		DeviceID:   deviceID,
		DeviceName: deviceName,
		FileName:   fileName,
		FileSize:   fileSize,
		Direction:  "sent",
		Status:     "requesting",
		Time:       time.Now(),
	}

	baseURL := fmt.Sprintf("http://%s:%d", deviceIP, devicePort)
	client := &http.Client{Timeout: 65 * time.Second}

	// Step 1: Send transfer request
	reqBody := models.TransferRequest{
		ID:         record.ID,
		SenderID:   localDevice.ID,
		SenderName: localDevice.Name,
		SenderIP:   localDevice.IP,
		FileName:   fileName,
		FileSize:   fileSize,
	}
	reqData, _ := json.Marshal(reqBody)

	resp, err := client.Post(baseURL+"/request", "application/json", bytes.NewReader(reqData))
	if err != nil {
		record.Status = "failed"
		return record, fmt.Errorf("send request: %w", err)
	}

	var transferResp models.TransferResponse
	if err := json.NewDecoder(resp.Body).Decode(&transferResp); err != nil {
		resp.Body.Close()
		record.Status = "failed"
		return record, fmt.Errorf("read response: %w", err)
	}
	resp.Body.Close()

	if !transferResp.Accepted {
		record.Status = "rejected"
		return record, fmt.Errorf("transfer rejected by receiver")
	}

	// Step 2: Receiver accepted, now send the file
	file, err := os.Open(filePath)
	if err != nil {
		record.Status = "failed"
		return record, fmt.Errorf("open file: %w", err)
	}
	defer file.Close()

	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	part, _ := writer.CreateFormFile("file", fileName)
	io.Copy(part, file)
	writer.WriteField("senderId", localDevice.ID)
	writer.WriteField("senderName", localDevice.Name)
	writer.Close()

	req, _ := http.NewRequest("POST", baseURL+"/upload", &buf)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp2, err := client.Do(req)
	if err != nil {
		record.Status = "failed"
		return record, fmt.Errorf("upload file: %w", err)
	}
	resp2.Body.Close()

	if resp2.StatusCode != http.StatusOK {
		record.Status = "failed"
		return record, fmt.Errorf("upload failed with status %d", resp2.StatusCode)
	}

	record.Status = "completed"
	return record, nil
}

// SendMessage sends a text message to a remote device with retry + ack
func SendMessage(deviceIP string, devicePort int, deviceID string, deviceName string, content string, sender models.Device) (*models.Message, error) {
	msg := &models.Message{
		ID:         uuid.NewString(),
		DeviceID:   deviceID,
		DeviceName: deviceName,
		Content:    content,
		Time:       time.Now(),
		Direction:  "sent",
	}

	body := map[string]string{
		"content":    content,
		"senderId":   sender.ID,
		"senderName": sender.Name,
	}
	data, _ := json.Marshal(body)

	url := fmt.Sprintf("http://%s:%d/message", deviceIP, devicePort)

	// Retry up to 3 times with backoff
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Duration(attempt) * 500 * time.Millisecond)
		}

		client := &http.Client{Timeout: 3 * time.Second}
		resp, err := client.Post(url, "application/json", bytes.NewReader(data))
		if err != nil {
			lastErr = fmt.Errorf("attempt %d: %w", attempt+1, err)
			continue
		}

		// Read ack
		var ack struct {
			Status string `json:"status"`
			ID     string `json:"id"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&ack); err != nil {
			resp.Body.Close()
			lastErr = fmt.Errorf("attempt %d: bad ack: %w", attempt+1, err)
			continue
		}
		resp.Body.Close()

		if ack.Status == "ok" {
			// Use server-assigned ID if available
			if ack.ID != "" {
				msg.ID = ack.ID
			}
			return msg, nil
		}
		lastErr = fmt.Errorf("attempt %d: status=%s", attempt+1, ack.Status)
	}

	return msg, fmt.Errorf("send failed after 3 retries: %w", lastErr)
}

// Ping checks if a device is reachable
func Ping(deviceIP string, devicePort int) bool {
	url := fmt.Sprintf("http://%s:%d/ping", deviceIP, devicePort)
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// SendFiles sends multiple files to a remote device
func SendFiles(deviceIP string, devicePort int, deviceID string, deviceName string, filePaths []string, localDevice models.Device) ([]models.TransferRecord, error) {
	var records []models.TransferRecord
	for _, fp := range filePaths {
		record, err := SendFile(deviceIP, devicePort, deviceID, deviceName, fp, localDevice)
		if record != nil {
			records = append(records, *record)
		}
		if err != nil {
			return records, err
		}
	}
	return records, nil
}

func getFileSize(path string) int64 {
	info, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return info.Size()
}
