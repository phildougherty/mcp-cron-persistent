// SPDX-License-Identifier: AGPL-3.0-only
package activity

import (
    "bytes"
    "encoding/json"
    "fmt"
    "log"
    "net/http"
    "os"
    "time"
)

// ActivityMessage represents an activity event
type ActivityMessage struct {
    ID        string                 `json:"id"`
    Timestamp string                 `json:"timestamp"`
    Level     string                 `json:"level"`
    Type      string                 `json:"type"`
    Server    string                 `json:"server"`
    Client    string                 `json:"client"`
    Message   string                 `json:"message"`
    Details   map[string]interface{} `json:"details"`
}

var webhookURL string

func init() {
    webhookURL = os.Getenv("MCP_CRON_ACTIVITY_WEBHOOK")
}

// BroadcastActivity sends an activity message to the dashboard
func BroadcastActivity(level, activityType, message string, details map[string]interface{}) {
    if webhookURL == "" {
        return // No webhook configured
    }

    activity := ActivityMessage{
        ID:        generateID(),
        Timestamp: time.Now().Format(time.RFC3339Nano),
        Level:     level,
        Type:      activityType,
        Server:    "task-scheduler",
        Client:    "",
        Message:   message,
        Details:   details,
    }

    go sendActivity(activity)
}

func sendActivity(activity ActivityMessage) {
    jsonData, err := json.Marshal(activity)
    if err != nil {
        log.Printf("[ACTIVITY] Failed to marshal activity: %v", err)
        return
    }

    resp, err := http.Post(webhookURL, "application/json", bytes.NewBuffer(jsonData))
    if err != nil {
        log.Printf("[ACTIVITY] Failed to send activity: %v", err)
        return
    }
    defer resp.Body.Close()

    if resp.StatusCode != http.StatusOK {
        log.Printf("[ACTIVITY] Unexpected response status: %d", resp.StatusCode)
    }
}

func generateID() string {
    return fmt.Sprintf("activity_%d", time.Now().UnixNano())
}
