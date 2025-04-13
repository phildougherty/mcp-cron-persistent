package utils

import (
	"reflect"
	"testing"
)

func TestJsonUnmarshal(t *testing.T) {
	type testStruct struct {
		Name  string `json:"name"`
		Value int    `json:"value"`
	}

	tests := []struct {
		name        string
		data        []byte
		target      interface{}
		expected    interface{}
		expectError bool
	}{
		{
			name:        "Valid JSON",
			data:        []byte(`{"name":"test","value":123}`),
			target:      &testStruct{},
			expected:    &testStruct{Name: "test", Value: 123},
			expectError: false,
		},
		{
			name:        "Empty data",
			data:        []byte{},
			target:      &testStruct{},
			expected:    &testStruct{},
			expectError: true,
		},
		{
			name:        "Invalid JSON",
			data:        []byte(`{"name":"test","value":invalid}`),
			target:      &testStruct{},
			expected:    &testStruct{},
			expectError: true,
		},
		{
			name:        "Incomplete JSON",
			data:        []byte(`{"name":"test"`),
			target:      &testStruct{},
			expected:    &testStruct{},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := JsonUnmarshal(tt.data, tt.target)

			if tt.expectError && err == nil {
				t.Errorf("Expected an error but got none")
				return
			}

			if !tt.expectError && err != nil {
				t.Errorf("Did not expect an error but got: %v", err)
				return
			}

			if !tt.expectError {
				if !reflect.DeepEqual(tt.target, tt.expected) {
					t.Errorf("Expected %+v but got %+v", tt.expected, tt.target)
				}
			}
		})
	}
}
