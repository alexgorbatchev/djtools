package engine

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestNullTime_Scan(t *testing.T) {
	tests := []struct {
		name      string
		input     any
		wantValid bool
		wantUnix  int64
		wantErr   bool
	}{
		{
			name:      "nil value",
			input:     nil,
			wantValid: false,
			wantUnix:  time.Time{}.Unix(),
			wantErr:   false,
		},
		{
			name:      "time.Time value",
			input:     time.Date(2025, 5, 10, 12, 0, 0, 0, time.UTC),
			wantValid: true,
			wantUnix:  time.Date(2025, 5, 10, 12, 0, 0, 0, time.UTC).Unix(),
			wantErr:   false,
		},
		{
			name:      "int64 epoch timestamp",
			input:     int64(1700000000),
			wantValid: true,
			wantUnix:  1700000000,
			wantErr:   false,
		},
		{
			name:      "int epoch timestamp",
			input:     int(1700000000),
			wantValid: true,
			wantUnix:  1700000000,
			wantErr:   false,
		},
		{
			name:      "int32 epoch timestamp",
			input:     int32(1700000000),
			wantValid: true,
			wantUnix:  1700000000,
			wantErr:   false,
		},
		{
			name:      "uint64 epoch timestamp",
			input:     uint64(1700000000),
			wantValid: true,
			wantUnix:  1700000000,
			wantErr:   false,
		},
		{
			name:      "float64 epoch timestamp",
			input:     float64(1700000000),
			wantValid: true,
			wantUnix:  1700000000,
			wantErr:   false,
		},
		{
			name:      "empty string",
			input:     "",
			wantValid: false,
			wantUnix:  time.Time{}.Unix(),
			wantErr:   false,
		},
		{
			name:      "formatted datetime string",
			input:     "2024-04-17 14:30:00",
			wantValid: true,
			wantUnix:  time.Date(2024, 4, 17, 14, 30, 0, 0, time.UTC).Unix(),
			wantErr:   false,
		},
		{
			name:      "RFC3339 string",
			input:     "2024-04-17T14:30:00Z",
			wantValid: true,
			wantUnix:  time.Date(2024, 4, 17, 14, 30, 0, 0, time.UTC).Unix(),
			wantErr:   false,
		},
		{
			name:      "stringified epoch timestamp",
			input:     "1700000000",
			wantValid: true,
			wantUnix:  1700000000,
			wantErr:   false,
		},
		{
			name:      "byte slice formatted date",
			input:     []byte("2024-04-17 14:30:00"),
			wantValid: true,
			wantUnix:  time.Date(2024, 4, 17, 14, 30, 0, 0, time.UTC).Unix(),
			wantErr:   false,
		},
		{
			name:      "invalid date string",
			input:     "not-a-date",
			wantValid: false,
			wantErr:   true,
		},
		{
			name:      "unsupported type bool",
			input:     true,
			wantValid: false,
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var nt nullTime
			err := nt.Scan(tt.input)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.wantValid, nt.Valid)
				assert.Equal(t, tt.wantUnix, nt.Time.Unix())

				val, valErr := nt.Value()
				assert.NoError(t, valErr)
				if tt.wantValid {
					assert.Equal(t, nt.Time, val)
				} else {
					assert.Nil(t, val)
				}
			}
		})
	}
}
