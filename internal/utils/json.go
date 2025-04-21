// SPDX-License-Identifier: AGPL-3.0-only
package utils

import (
	"encoding/json"
	"fmt"
)

// JsonUnmarshal unmarshals JSON data into a target struct
// This provides a centralized way to handle JSON unmarshaling in the application
func JsonUnmarshal(data []byte, target interface{}) error {
	if len(data) == 0 {
		return fmt.Errorf("empty JSON data")
	}

	err := json.Unmarshal(data, target)
	if err != nil {
		return fmt.Errorf("failed to unmarshal JSON: %w", err)
	}

	return nil
}
