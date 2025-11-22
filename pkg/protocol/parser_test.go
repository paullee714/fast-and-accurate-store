package protocol

import (
	"bufio"
	"strings"
	"testing"
)

func TestParseCommand(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    *Command
		wantErr bool
	}{
		{
			name:  "Simple SET",
			input: "SET key value\n",
			want: &Command{
				Name: "SET",
				Args: []string{"key", "value"},
			},
			wantErr: false,
		},
		{
			name:  "GET with one arg",
			input: "GET key\n",
			want: &Command{
				Name: "GET",
				Args: []string{"key"},
			},
			wantErr: false,
		},
		{
			name:  "Case Insensitive",
			input: "set key value\n",
			want: &Command{
				Name: "SET",
				Args: []string{"key", "value"},
			},
			wantErr: false,
		},
		{
			name:    "Empty Line",
			input:   "\n",
			want:    nil,
			wantErr: false,
		},
		{
			name:  "Multiple Spaces",
			input: "SET   key    value\n",
			want: &Command{
				Name: "SET",
				Args: []string{"key", "value"},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := bufio.NewReader(strings.NewReader(tt.input))
			got, err := ParseCommand(reader)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseCommand() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got == nil && tt.want == nil {
				return
			}
			if got == nil || tt.want == nil {
				t.Errorf("ParseCommand() = %v, want %v", got, tt.want)
				return
			}
			if got.Name != tt.want.Name {
				t.Errorf("ParseCommand() Name = %v, want %v", got.Name, tt.want.Name)
			}
			if len(got.Args) != len(tt.want.Args) {
				t.Errorf("ParseCommand() Args length = %v, want %v", len(got.Args), len(tt.want.Args))
			}
			for i := range got.Args {
				if got.Args[i] != tt.want.Args[i] {
					t.Errorf("ParseCommand() Arg[%d] = %v, want %v", i, got.Args[i], tt.want.Args[i])
				}
			}
		})
	}
}
